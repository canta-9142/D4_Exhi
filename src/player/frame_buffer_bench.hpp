#ifndef FRAME_BUFFER_BENCH_HPP_
#define FRAME_BUFFER_BENCH_HPP_

// -DPLAYER_FRAMEBUFFER_BENCHで選択する実機ベンチ。通常のUSBプロトコルとは別モード。
#include <Arduino.h>
#include <algorithm>
#include <array>
#include <esp_memory_utils.h>
#include "display.hpp"
#include "frame_buffer.hpp"
#include "frame_damage.hpp"

namespace framebuffer_bench {

constexpr int BLOCK_WIDTH = 16;
constexpr int SAMPLE_COUNT = 32;
constexpr size_t DMA_PIXELS = BLOCK_WIDTH * FrameBuffer::HEIGHT;
FrameBuffer frame;
std::array<lgfx::swap565_t*, 2> dma{};
bool initialized = false;
char mode = 's';
uint32_t sequence = 0, previousFrameMs = 0;
bool forceFull = true;
static_assert(FrameDamage::WIDTH == FrameBuffer::WIDTH && FrameDamage::HEIGHT == FrameBuffer::HEIGHT);
static_assert(FrameDamage::TILE == BLOCK_WIDTH);

struct Area { int x, y, w, h; };
constexpr Area FULL{0, 0, FrameBuffer::WIDTH, FrameBuffer::HEIGHT};
constexpr Area PARTIAL{ANIMATION_X, ANIMATION_Y, ANIMATION_WIDTH, ANIMATION_HEIGHT};

struct Sample {
  uint32_t drawUs = 0, packUs = 0, submitUs = 0, waitUs = 0, transferUs = 0, frameUs = 0;
};

void error(const char* reason) {
  Serial.printf("{\"type\":\"bench_error\",\"data\":{\"reason\":\"%s\"}}\n", reason);
}

uint32_t checksum(const lgfx::swap565_t* pixels) {
  uint32_t value = 2166136261u;
  for (size_t i = 0; i < FrameBuffer::PIXELS; ++i) value = (value ^ pixels[i].raw) * 16777619u;
  return value;
}

uint16_t testPixel(int x, int y, int page) {
  return uint16_t(x * 73 ^ y * 151 ^ page * 0xA55A);
}

bool selfTest() {
  for (int page = 0; page < 2; ++page) {
    for (int y = 0; y < FrameBuffer::HEIGHT; ++y)
      for (int x = 0; x < FrameBuffer::WIDTH; ++x)
        frame.back()[y * FrameBuffer::WIDTH + x].raw = testPixel(x, y, page);
    frame.swap();
    // 非ゼロの原点、端、16列未満の最終ブロックも検証する。
    for (const Area area : {FULL, PARTIAL, Area{7, 9, 37, 23}, Area{477, 317, 3, 3}}) {
      for (int x = 0; x < area.w; x += BLOCK_WIDTH) {
        const int width = std::min(BLOCK_WIDTH, area.w - x);
        frame.copyFrontColumns(area.x + x, area.y, width, area.h, dma[0]);
        for (int col = 0; col < width; ++col)
          for (int row = 0; row < area.h; ++row)
            if (dma[0][col * area.h + row].raw != testPixel(area.x + x + col, area.y + row, page)) return false;
      }
    }
    const auto before = checksum(frame.front());
    frame.fillScreen(TFT_WHITE);
    if (before != checksum(frame.front())) return false;
  }
  frame.fillScreen(TFT_BLACK);
  frame.swap();
  frame.fillScreen(TFT_BLACK);
  frame.drawPixel(0, 0, TFT_RED);
  frame.drawPixel(479, 319, TFT_BLUE);
  lgfx::swap565_t red, blue;
  red = lgfx::rgb565_t(TFT_RED);
  blue = lgfx::rgb565_t(TFT_BLUE);
  return frame.back()[0] == red && frame.back()[FrameBuffer::PIXELS - 1] == blue;
}

void render(const Area& area, uint32_t number, bool pattern = false) {
  frame.startWrite();
  frame.fillRect(area.x, area.y, area.w, area.h, TFT_BLACK);
  for (int x = 0; x < area.w; x += 32)
    frame.drawFastVLine(area.x + x, area.y, area.h, TFT_DARKGREY);
  for (int y = 0; y < area.h; y += 32)
    frame.drawFastHLine(area.x, area.y + y, area.w, TFT_DARKGREY);
  if (pattern) {
    frame.fillRect(area.x + 24, area.y + 64, area.w - 48, area.h - 104,
                   number & 1 ? TFT_MAGENTA : TFT_CYAN);
  } else {
    const int position = number * 7 % (area.w + 32) - 32;
    frame.setClipRect(area.x, area.y, area.w, area.h);
    frame.fillRect(area.x + position, area.y, 32, area.h, TFT_WHITE);
    frame.clearClipRect();
  }
  frame.fillRect(area.x, area.y, 20, 20, TFT_RED);
  frame.fillRect(area.x + area.w - 20, area.y, 20, 20, TFT_GREEN);
  frame.fillRect(area.x, area.y + area.h - 20, 20, 20, TFT_BLUE);
  frame.fillRect(area.x + area.w - 20, area.y + area.h - 20, 20, 20, TFT_YELLOW);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(2);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.setCursor(area.x + 24, area.y + 24);
  frame.printf("%s %c %lu", area.w == FULL.w ? "FULL" : "PARTIAL", number & 1 ? 'B' : 'A',
               (unsigned long)number);
  frame.endWrite();
}

// swap後のfrontは転送完了まで不変。次画像はbackにだけ描く。
Sample present(const Area& area, uint32_t nextNumber, bool pipeline, bool pattern = false) {
  Sample result;
  const uint32_t started = micros();
  frame.swap();
  const auto drawNext = [&] {
    const uint32_t drawStarted = micros();
    render(area, nextNumber, pattern);
    result.drawUs = micros() - drawStarted;
  };
  if (!pipeline) drawNext();
  const uint32_t transferStarted = micros();
  lcd.beginColumnWrite();
  unsigned block = 0;
  for (int x = 0; x < area.w; x += BLOCK_WIDTH, ++block) {
    auto* buffer = dma[block % dma.size()];
    const int width = std::min(BLOCK_WIDTH, area.w - x);
    uint32_t stamp = micros();
    frame.copyFrontColumns(area.x + x, area.y, width, area.h, buffer);
    result.packUs += micros() - stamp;
    stamp = micros();
    lcd.waitDMA(); // 別のバッファを準備してから、前ブロックの完了を待つ。
    result.waitUs += micros() - stamp;
    stamp = micros();
    lcd.pushColumnBlockDMA(area.x + x, area.y, width, area.h, buffer);
    result.submitUs += micros() - stamp;
    if (block == 0 && pipeline) drawNext();
    if (!pipeline) {
      stamp = micros();
      lcd.waitDMA();
      result.waitUs += micros() - stamp;
    }
  }
  const uint32_t waitStarted = micros();
  lcd.endColumnWrite(); // 最後のDMAを含め、swap前に必ず完了させる。
  result.waitUs += micros() - waitStarted;
  result.transferUs = micros() - transferStarted;
  result.frameUs = micros() - started;
  return result;
}

enum class Scene { Still, Small, Scattered, Full };

const char* sceneName(Scene scene) {
  switch (scene) {
    case Scene::Still: return "static";
    case Scene::Small: return "small";
    case Scene::Scattered: return "scattered";
    case Scene::Full: return "full";
  }
  return "unknown";
}

void renderScene(Scene scene, uint32_t number) {
  if (scene == Scene::Full) {
    frame.fillScreen(number & 1 ? TFT_MAGENTA : TFT_CYAN);
    return;
  }
  render(FULL, 0);
  frame.setCursor(24, 48);
  frame.print(sceneName(scene));
  if (scene == Scene::Small) {
    frame.fillRect(number * 7 % (FrameBuffer::WIDTH - 32), 144, 32, 32, TFT_WHITE);
  } else if (scene == Scene::Scattered) {
    const uint16_t color = number & 1 ? TFT_ORANGE : TFT_WHITE;
    for (const auto area : {Area{0, 0, 16, 16}, Area{464, 304, 16, 16},
                            Area{32, 240, 16, 16}, Area{400, 32, 16, 16}})
      frame.fillRect(area.x, area.y, area.w, area.h, color);
  }
}

struct DiffSample {
  Sample timing;
  uint32_t compareUs = 0, bytes = 0, spans = 0, dirtyTiles = 0;
};

// backの描画完了後に呼ぶ。旧frontの比較が済むまでswapしない。
DiffSample presentDifference(bool differential, bool forced) {
  DiffSample result;
  const uint32_t started = micros();
  FrameDamage damage;
  if (differential && !forced) {
    const uint32_t compareStarted = micros();
    damage.compare(frame.front(), frame.back());
    result.compareUs = micros() - compareStarted;
  } else {
    damage.full();
  }
  result.dirtyTiles = damage.changedTiles();
  frame.swap();
  if (result.dirtyTiles) {
    const uint32_t transferStarted = micros();
    lcd.beginColumnWrite();
    damage.forEachSpan([&](int x, int y, int w, int h) {
      auto* buffer = dma[result.spans % dma.size()];
      uint32_t stamp = micros();
      frame.copyFrontColumns(x, y, w, h, buffer);
      result.timing.packUs += micros() - stamp;
      stamp = micros();
      lcd.waitDMA();
      result.timing.waitUs += micros() - stamp;
      stamp = micros();
      lcd.pushColumnBlockDMA(x, y, w, h, buffer);
      result.timing.submitUs += micros() - stamp;
      result.bytes += w * h * sizeof(lgfx::swap565_t);
      ++result.spans;
    });
    const uint32_t waitStarted = micros();
    lcd.endColumnWrite();
    result.timing.waitUs += micros() - waitStarted;
    result.timing.transferUs = micros() - transferStarted;
  }
  result.timing.frameUs = micros() - started;
  return result;
}

void benchmarkDifference() {
  mode = 's';
  for (const Scene scene : {Scene::Still, Scene::Small, Scene::Scattered, Scene::Full}) {
    for (const bool differential : {false, true}) {
      std::array<DiffSample, SAMPLE_COUNT> samples;
      for (unsigned i = 0; i < samples.size() + 2; ++i) {
        const auto expected = checksum(frame.front());
        const uint32_t drawStarted = micros();
        renderScene(scene, i);
        const uint32_t drawUs = micros() - drawStarted;
        if (checksum(frame.front()) != expected) { error("front changed during scene drawing"); return; }
        auto result = presentDifference(differential, i == 0);
        result.timing.drawUs = drawUs;
        result.timing.frameUs += drawUs;
        if (i >= 2) samples[i - 2] = result;
        delay(1);
      }
      for (const auto& r : samples) {
        const auto& t = r.timing;
        Serial.printf("{\"type\":\"diff_sample\",\"data\":{\"scene\":\"%s\",\"strategy\":\"%s\","
                      "\"draw_us\":%lu,\"compare_us\":%lu,\"pack_us\":%lu,\"submit_us\":%lu,\"wait_us\":%lu,"
                      "\"transfer_us\":%lu,\"frame_us\":%lu,\"bytes\":%lu,\"spans\":%lu,\"dirty_tiles\":%lu}}\n",
                      sceneName(scene), differential ? "diff" : "full",
                      (unsigned long)t.drawUs, (unsigned long)r.compareUs, (unsigned long)t.packUs,
                      (unsigned long)t.submitUs, (unsigned long)t.waitUs, (unsigned long)t.transferUs,
                      (unsigned long)t.frameUs, (unsigned long)r.bytes, (unsigned long)r.spans, (unsigned long)r.dirtyTiles);
      }
    }
  }
  Serial.println("{\"type\":\"diff_done\",\"data\":{\"samples\":256,\"front_unchanged\":true}}");
}

void ready() {
  Serial.printf("{\"type\":\"bench_ready\",\"data\":{\"ok\":%s,\"width\":480,\"height\":320,"
                "\"psram_bytes\":%u,\"dma_bytes\":%u,\"block_width\":16,\"self_test\":%s}}\n",
                initialized ? "true" : "false", unsigned(2 * FrameBuffer::BYTES),
                unsigned(2 * DMA_PIXELS * sizeof(lgfx::swap565_t)), initialized ? "true" : "false");
}

void benchmark() {
  mode = 's';
  for (const Area area : {PARTIAL, FULL}) {
    for (const bool pipeline : {false, true}) {
      render(area, 0);
      std::array<Sample, SAMPLE_COUNT> samples;
      for (unsigned i = 0; i < samples.size() + 2; ++i) {
        const auto expected = checksum(frame.back());
        const auto result = present(area, i + 1, pipeline);
        if (checksum(frame.front()) != expected) { error("front changed during back drawing"); return; }
        if (i >= 2) samples[i - 2] = result;
        delay(1);
      }
      for (unsigned i = 0; i < samples.size(); ++i) {
        const auto& s = samples[i];
        Serial.printf("{\"type\":\"bench_sample\",\"data\":{\"width\":%d,\"height\":%d,\"pipeline\":%s,"
                      "\"draw_us\":%lu,\"pack_us\":%lu,\"submit_us\":%lu,\"wait_us\":%lu,"
                      "\"transfer_us\":%lu,\"frame_us\":%lu}}\n", area.w, area.h, pipeline ? "true" : "false",
                      (unsigned long)s.drawUs, (unsigned long)s.packUs, (unsigned long)s.submitUs,
                      (unsigned long)s.waitUs, (unsigned long)s.transferUs, (unsigned long)s.frameUs);
      }
    }
  }
  Serial.println("{\"type\":\"bench_done\",\"data\":{\"samples\":128,\"front_unchanged\":true}}");
}

void enter(char command) {
  if (!initialized) { error("initialization failed"); return; }
  if (command == '?') { ready(); return; }
  if (command == 'b') { benchmark(); return; }
  if (command == 'd') { benchmarkDifference(); return; }
  if (command == 'u' || command == 'm' || command == 'n' || command == 'c') {
    mode = command;
    sequence = 0;
    forceFull = true;
    Serial.printf("{\"type\":\"bench_mode\",\"data\":{\"mode\":\"%c\"}}\n", mode);
    return;
  }
  if (command != 'p' && command != 'f' && command != 'r' && command != 's') return;
  mode = command;
  sequence = 0;
  frame.fillScreen(TFT_BLACK);
  frame.swap();
  frame.fillScreen(TFT_BLACK);
  lcd.fillScreen(TFT_BLACK);
  if (mode != 's') render(mode == 'r' ? PARTIAL : FULL, sequence, mode == 'p');
  Serial.printf("{\"type\":\"bench_mode\",\"data\":{\"mode\":\"%c\"}}\n", mode);
}

void begin() {
  Serial.setRxBufferSize(1024);
  Serial.setTxBufferSize(16384);
  Serial.setTxTimeoutMs(1000);
  Serial.begin(115200);
  Serial.setDebugOutput(false);
  if (!lcd.init()) { error("LCD initialization failed"); return; }
  lcd.setRotation(1);
  lcd.setColorDepth(16);
  lcd.fillScreen(TFT_BLACK);
  if (!frame.begin()) { error("PSRAM allocation failed"); return; }
  for (auto& buffer : dma) {
    buffer = static_cast<lgfx::swap565_t*>(heap_caps_malloc(DMA_PIXELS * sizeof(lgfx::swap565_t),
                                                          MALLOC_CAP_INTERNAL | MALLOC_CAP_DMA));
    if (!buffer) { error("DMA allocation failed"); return; }
  }
  if (!esp_ptr_external_ram(frame.front()) || !esp_ptr_external_ram(frame.back()) ||
      !selfTest()) { error("framebuffer self test failed"); return; }
  initialized = true;
  enter('p');
  ready();
}

void update() {
  while (Serial.available()) enter(Serial.read());
  if (initialized && mode != 's') {
    if (mode == 'u' || mode == 'm' || mode == 'n' || mode == 'c') {
      const uint32_t intervalMs = mode == 'c' || mode == 'n' ? 250 : 16;
      if (millis() - previousFrameMs >= intervalMs) {
        previousFrameMs = millis();
        const Scene scene = mode == 'u' ? Scene::Still : mode == 'm' ? Scene::Small :
                            mode == 'n' ? Scene::Scattered : Scene::Full;
        renderScene(scene, sequence++);
        presentDifference(true, forceFull);
        forceFull = false;
      }
      delay(1);
      return;
    }
    const uint32_t intervalMs = mode == 'p' ? 1000 : 16;
    if (millis() - previousFrameMs >= intervalMs) {
      previousFrameMs = millis();
      present(mode == 'r' ? PARTIAL : FULL, ++sequence, true, mode == 'p');
    }
  }
  delay(1);
}

} // namespace framebuffer_bench

void setup() { framebuffer_bench::begin(); }
void loop() { framebuffer_bench::update(); }

#endif // FRAME_BUFFER_BENCH_HPP_
