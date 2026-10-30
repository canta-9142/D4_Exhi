#ifndef FPS_DEMO_HPP_
#define FPS_DEMO_HPP_

// -DPLAYER_PLAYING_BENCH: 通常と同じ描画経路を計測し、全描画との一致も検証する。
#include "drawing.hpp"
#include <cstring>

namespace playing_bench {
constexpr unsigned SAMPLES = 32;
constexpr unsigned SCENES = 11;
constexpr unsigned EFFECTS_SCENE = 10;
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
  if (scene == 1 || scene == 4 || scene == EFFECTS_SCENE) {
    if (index == 8 || index == 24) queue.pop();
    animation.update(queue, 16, ANIMATION_HEIGHT);
    s.cards = animation.snapshot();
  }
  if (scene == EFFECTS_SCENE) {
    // cardsと同じカードの動きに、毎フレーム進む演出を重ねる（炎あり・なしの比較）。左の列と右の余白を毎回描き直す最悪の場合。
    // 炎の格子は描画側が持つので、全描画との比較でも同じ格子になるよう炎の段は固定し、正規化と火の粉で右の余白も描き直す。
    s.status.combo = 25;
    s.status.rank = 1;
    EffectsFrame& f = s.effects;
    f.epoch = 1;
    f.flameStep = FLAME_ROWS;
    f.flameLit = true;
    f.flameFuel = 232;
    f.flameScale = uint8_t(232 - index % 2);
    f.flameRows = 58;
    f.flameHot = true;
    f.sparks = true;
    f.sparkAge = 100 + index;
    f.form = MascotForm::Blaze;
    f.flightMs = index * FLAME_STEP_MS;
    f.blink = index % 8 == 3 ? 1 : 0;
    if (index >= 8 && index < 17) {
      f.milestoneMs = uint16_t((index - 8) * FLAME_STEP_MS);
      f.milestone = 20;
    }
    if (index >= 12 && index < 24) {
      f.rankUpMs = uint16_t((index - 12) * FLAME_STEP_MS);
      f.rankUpRank = 1;
      s.rankFlash = f.rankUpMs / RANK_BLINK_MS % 2 == 0;
    }
    if (index >= 24) {
      // コンボが切れた: しょんぼりの顔で落ちて煙になる。BREAKと炎の煙も出す。
      s.status.combo = 0;
      f.form = MascotForm::None;
      f.sparks = false;
      f.flameFuel = 0;
      f.flameRows = 0;
      f.flameHot = false;
      f.breakMs = uint16_t((index - 24) * FLAME_STEP_MS * 2);
      f.brokenForm = MascotForm::Blaze;
      f.brokenFlightMs = 24 * FLAME_STEP_MS;
      f.smokeMs = uint16_t(std::min<unsigned>((index - 24) * FLAME_STEP_MS, SMOKE_MS - 1));
      f.smokeRows = 58;
      if (f.breakMs < BREAK_TEXT_MS) f.breakTextMs = f.breakMs;
    }
  }
  if (scene == 2 || scene == 4) s.pressedMask = index & 1 ? 5 : 0;
  if (scene == 3 || scene == 4) {
    constexpr int32_t scores[] = {0, 9, 10, 99, 100, 1000, 2147483647, 0};
    s.status = {scores[index % 8], uint8_t(120 - index), bool(index & 1), index % 8 != 0, 0, uint8_t(index % 9)};
    s.awaitingStatus = !s.status.known;
    // 順位の点滅と、列ごとに年齢の違う獲得点（発信者・中継・明るさの段階）も部分描画で検証する。
    constexpr uint8_t gains[] = {15, 8, 10};
    s.rankFlash = index & 2;
    for (unsigned i = 0; i < s.gains.size(); ++i)
      s.gains[i] = {gains[(index + i) % 3], uint16_t((index * 97 + i * 500) % GAIN_DURATION_MS), (index + i) % 4 != 0};
  }
  if (scene == 4 && index >= 16) s.selfId = NodeId::P8;
  if (scene == 5 || scene == 6) {
    s.phase = GamePhase::Preparing;
    s.status.ready = false;
    s.readyHoldMs = std::min<unsigned>(index * 80, ReadyHold::DURATION_MS);
    s.pressedMask = index ? ReadyHold::END_BUTTONS : 0;
    if (scene == 5) {
      s.readyPending = index >= 26;
      s.status.ready = index >= 30;
    } else {
      s.readyPending = index >= 8 && index < 12;
      s.status.ready = index >= 12 && index < 16;
      if (index >= 16 && index < 20) s.phase = GamePhase::Playing;
      if (index >= 20 && index < 24) s.mode = ControlMode::Auto;
      if (index >= 24 && index < 28) { s.synced = false; s.awaitingStatus = true; }
      if (index >= 28) { s.readyHoldMs = 0; s.pressedMask = 0; }
    }
  }
  if (scene >= 7 && scene < EFFECTS_SCENE) {
    s.phase = GamePhase::Finished;
    s.status = {INT32_MAX, 0, true, true};
    s.summary = {INT32_MAX, 2, true, true, 1, true};
    // ウォームアップ2枚の後も、32サンプルすべてで演出の進捗を更新する。
    s.finishedProgress = std::min<unsigned>(index, SAMPLES + 1) * 1000 / (SAMPLES + 1);
    if (scene == 8) {
      s.finishedProgress = 1000;
      s.summary.score = index % 2 ? INT32_MAX : 0;
      s.summary.eligible = index / 8 != 1;
      s.summary.rank = s.summary.eligible ? uint8_t(index % 8 + 1) : 0;
      s.mode = index & 1 ? ControlMode::Manual : ControlMode::Auto;
      if (index / 8 == 2) {
        if (index % 8 == 0) s.synced = false;
        if (index % 8 == 1) s.awaitingStatus = true;
        if (index % 8 == 2) s.summary.known = false;
      }
      if (index / 8 == 3) {
        if (index % 8 == 6) s.phase = GamePhase::Preparing;
        if (index % 8 == 7) s.phase = GamePhase::Playing;
      }
    }
    if (scene == 9) {
      s.preview = index < 16 || index % 8 < 6;
      s.previewLit = index % 2 == 0;
      s.selfId = NodeId(index / 2 % 8 + 1);
      if (index >= 16 && index % 8 == 6) s.phase = GamePhase::Preparing;
    }
  }
  return s;
}

bool verify(const DisplaySnapshot& s) {
  // 検証中だけbackを全描画用に使い、検証後は内容と描画履歴をそのまま復元する。
  std::memcpy(reference, frame.back(), FrameBuffer::BYTES);
  bool valid = true;
  if (backDisplayDrawn) {
    draw_screen_layout(previousBackDisplay);
    render_snapshot(previousBackDisplay, display_changes({}, previousBackDisplay, true));
    valid = !std::memcmp(reference, frame.back(), FrameBuffer::BYTES);
  }
  draw_screen_layout(s);
  render_snapshot(s, display_changes({}, s, true));
  valid = valid && !std::memcmp(frame.front(), frame.back(), FrameBuffer::BYTES);
  std::memcpy(frame.back(), reference, FrameBuffer::BYTES);
  return valid;
}

void report_ready() {
  Serial.printf("{\"type\":\"playing_ready\",\"data\":{\"ok\":%s,\"psram_bytes\":614400,\"dma_bytes\":20480}}\n", ready ? "true" : "false");
}

void benchmark() {
  constexpr const char* names[SCENES] = {"idle", "cards", "buttons", "numbers", "mixed", "preparing", "phases", "finished", "results", "preview", "effects"};
  for (unsigned scene = 0; scene < SCENES; ++scene) {
    queue.clear();
    animation = PacketAnimation{};
    for (unsigned i = 0; i < 6; ++i) queue.push({uint16_t(1001 + i), NodeId(unsigned(NodeId::P2) + i)});
    // 演出の場面では、最初のフレームで炎を上端まで育てる（同じ格子のまま場面を通す）。
    flameEpoch = 1;
    flameLeft.reset();
    flameRight.reset();
    draw_static_layout();
    displayDrawn = false;
    backDisplayDrawn = false;
    draw_snapshot(snapshot(0, 0));
    std::array<DisplayFrameStats, SAMPLES> samples;
    for (unsigned i = 0; i < SAMPLES + 2; ++i) {
      const auto s = snapshot(scene, i);
      const bool normalFinished = scene != 7 ||
          (display_screen(s) == DisplayScreen::Finished && finished_state(s) == FinishedState::Ready &&
           s.mode == ControlMode::Manual && s.summary.score == INT32_MAX &&
           s.summary.eligible && s.summary.rank == 2);
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
      if (!normalFinished || !covered || !verify(s)) {
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
  Serial.printf("{\"type\":\"playing_done\",\"data\":{\"samples\":%u,\"reference_match\":true,\"buffer_states_valid\":true}}\n",
                SCENES * SAMPLES);
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
