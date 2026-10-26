#ifndef FPS_DEMO_HPP_
#define FPS_DEMO_HPP_

// -DPLAYER_PLAYING_BENCH: 通常と同じ描画経路を計測し、全描画との一致も検証する。
#include "drawing.hpp"
#include <cstring>

namespace playing_bench {
constexpr unsigned SAMPLES = 32;
bool ready = false;
lgfx::swap565_t* reference = nullptr;
PacketQueue queue;
PacketAnimation animation;

DisplaySnapshot snapshot(unsigned scene, unsigned index) {
  DisplaySnapshot s{};
  s.selfId = NodeId::P1;
  s.phase = GamePhase::Playing;
  s.mode = ControlMode::Manual;
  s.synced = true;
  s.status = {123, 120, true, true};
  for (unsigned i = 0; i < 3; ++i)
    s.cards[i] = {{uint16_t(1001 + i), NodeId(unsigned(NodeId::P2) + i)}, float(170 - i * 80), int(170 - i * 80), true, PACKET_FADE_MS};
  if (scene == 1 || scene == 4) {
    if (index == 8 || index == 24) queue.pop();
    animation.update(queue, 16, ANIMATION_HEIGHT);
    s.cards = animation.snapshot();
  }
  if (scene == 2 || scene == 4) s.pressedMask = index & 1 ? 5 : 0;
  if (scene == 3 || scene == 4) {
    constexpr int32_t scores[] = {0, 9, 10, 99, 100, 1000, 2147483647, 0};
    s.status = {scores[index % 8], uint8_t(120 - index), bool(index & 1), index % 8 != 0};
    s.awaitingStatus = !s.status.known;
  }
  if (scene == 4 && index >= 16) s.selfId = NodeId::P8;
  return s;
}

bool verify(const DisplaySnapshot& s) {
  // 検証中だけbackを全描画用に使い、検証後は内容と描画履歴をそのまま復元する。
  std::memcpy(reference, frame.back(), FrameBuffer::BYTES);
  bool valid = true;
  if (backDisplayDrawn) {
    draw_static_layout();
    render_snapshot(previousBackDisplay, display_changes({}, previousBackDisplay, true));
    valid = !std::memcmp(reference, frame.back(), FrameBuffer::BYTES);
  }
  draw_static_layout();
  render_snapshot(s, display_changes({}, s, true));
  valid = valid && !std::memcmp(frame.front(), frame.back(), FrameBuffer::BYTES);
  std::memcpy(frame.back(), reference, FrameBuffer::BYTES);
  return valid;
}

void report_ready() {
  Serial.printf("{\"type\":\"playing_ready\",\"data\":{\"ok\":%s,\"psram_bytes\":614400,\"dma_bytes\":20480}}\n", ready ? "true" : "false");
}

void benchmark() {
  constexpr const char* names[] = {"idle", "cards", "buttons", "numbers", "mixed"};
  for (unsigned scene = 0; scene < 5; ++scene) {
    queue.clear();
    animation = PacketAnimation{};
    for (unsigned i = 0; i < 6; ++i) queue.push({uint16_t(1001 + i), NodeId(unsigned(NodeId::P2) + i)});
    draw_static_layout();
    displayDrawn = false;
    backDisplayDrawn = false;
    draw_snapshot(snapshot(0, 0));
    std::array<DisplayFrameStats, SAMPLES> samples;
    for (unsigned i = 0; i < SAMPLES + 2; ++i) {
      const auto s = snapshot(scene, i);
      const auto scheduled = display_damage(display_changes(previousDisplay, s, false), false);
      const auto stats = draw_snapshot(s);
      bool covered = true;
      if (stats.bytes) {
        FrameDamage actual;
        actual.compare(frame.back(), frame.front());
        actual.forEachSpan([&](int x, int y, int, int h) {
          for (int row = y; row < y + h; row += FrameDamage::TILE)
            if (!scheduled.containsPixel(x, row)) covered = false;
        });
      }
      if (!covered || !verify(s)) {
        Serial.printf("{\"type\":\"playing_error\",\"data\":{\"scene\":\"%s\",\"index\":%u}}\n", names[scene], i);
        return;
      }
      if (i >= 2) samples[i - 2] = stats;
      delay(1);
    }
    for (const auto& t : samples)
      Serial.printf("{\"type\":\"playing_sample\",\"data\":{\"scene\":\"%s\",\"draw_us\":%lu,\"pack_us\":%lu,"
                    "\"scan_read_us\":%lu,\"scan_wait_us\":%lu,\"transfer_us\":%lu,\"frame_us\":%lu,\"bytes\":%lu,\"spans\":%lu}}\n",
                    names[scene], (unsigned long)t.drawUs, (unsigned long)t.packUs, (unsigned long)t.scanReadUs,
                    (unsigned long)t.scanWaitUs, (unsigned long)t.transferUs, (unsigned long)t.frameUs,
                    (unsigned long)t.bytes, (unsigned long)t.spans);
  }
  Serial.println("{\"type\":\"playing_done\",\"data\":{\"samples\":160,\"reference_match\":true,\"buffer_states_valid\":true}}");
}
}

void setup() {
  Serial.setRxBufferSize(1024);
  Serial.setTxBufferSize(16384);
  Serial.setTxTimeoutMs(1000);
  Serial.begin(115200);
  Serial.setDebugOutput(false);
  using namespace playing_bench;
  reference = static_cast<lgfx::swap565_t*>(heap_caps_malloc(FrameBuffer::BYTES, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
  ready = reference && initialize_display(false);
  report_ready();
}

void loop() {
  while (Serial.available()) {
    const char c = Serial.read();
    if (c == '?') playing_bench::report_ready();
    if (c == 'b' && playing_bench::ready) playing_bench::benchmark();
  }
  delay(1);
}

#endif // FPS_DEMO_HPP_
