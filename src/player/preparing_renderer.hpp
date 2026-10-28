#ifndef PREPARING_RENDERER_HPP_
#define PREPARING_RENDERER_HPP_

#include "frame_buffer.hpp"
#include "display_snapshot.hpp"

inline void preparing_text(FrameBuffer& frame, const char* text, int y, int size = 2) {
  frame.setFont(&fonts::Font0);
  frame.setTextSize(size);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString(text, (FrameBuffer::WIDTH - frame.textWidth(text)) / 2, y);
}

inline const char* preparing_wait_label(const DisplaySnapshot& s) {
  if (s.selfId == NodeId::None) return "CONNECTING";
  if (!s.synced) return "SYNCING";
  return "WAIT STATUS";
}

inline void preparing_start_wait(FrameBuffer& frame, int y) {
  preparing_text(frame, "Waiting for the", y);
  preparing_text(frame, "administrator to start.", y + 24);
}

inline void draw_preparing_layout(FrameBuffer& frame) {
  frame.fillScreen(TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(3);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString("PREPARING", 24, 20);
  frame.drawFastHLine(0, 80, FrameBuffer::WIDTH, TFT_WHITE);
}

inline void draw_preparing_bar(FrameBuffer& frame, const DisplaySnapshot& s) {
  const auto state = preparing_state(s);
  if (state != PreparingState::Manual && state != PreparingState::Confirming && state != PreparingState::Ready) return;
  const unsigned progress = state == PreparingState::Manual ?
      std::min<unsigned>(s.readyHoldMs, ReadyHold::DURATION_MS) : ReadyHold::DURATION_MS;
  frame.fillRect(40, 152, 400, 32, TFT_BLACK);
  frame.drawRect(40, 152, 400, 32, TFT_WHITE);
  const int width = progress * 392 / ReadyHold::DURATION_MS;
  if (width) frame.fillRect(44, 156, width, 24, TFT_WHITE);
}

inline void draw_preparing_arrows(FrameBuffer& frame, const DisplaySnapshot& s) {
  if (preparing_state(s) != PreparingState::Manual) return;
  for (unsigned side = 0; side < 2; ++side) {
    const int x = side ? 420 : 60;
    const uint8_t bit = side ? 8 : 1;
    const uint16_t color = s.pressedMask & bit ? TFT_WHITE : TFT_DARKGREY;
    frame.fillRect(x - 16, 264, 32, 52, TFT_BLACK);
    frame.fillRect(x - 2, 264, 4, 36, color);
    frame.fillTriangle(x - 12, 296, x + 12, 296, x, 312, color);
  }
}

inline void draw_preparing_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(0, 81, 480, 239, TFT_BLACK);
  switch (preparing_state(s)) {
    case PreparingState::Waiting:
      preparing_text(frame, preparing_wait_label(s), 128, 3);
      preparing_text(frame, "Please wait.", 184);
      break;
    case PreparingState::Auto:
      preparing_text(frame, "AUTO", 120, 4);
      preparing_start_wait(frame, 184);
      break;
    case PreparingState::Manual:
      preparing_text(frame, "Press and hold both end buttons.", 104);
      preparing_text(frame, "Hold for 2 seconds.", 208);
      break;
    case PreparingState::Confirming:
      preparing_text(frame, "CONFIRMING...", 104, 3);
      preparing_text(frame, "Waiting for confirmation.", 208);
      break;
    case PreparingState::Ready:
      preparing_text(frame, "READY", 104, 4);
      preparing_start_wait(frame, 208);
      break;
  }
  draw_preparing_bar(frame, s);
  draw_preparing_arrows(frame, s);
}

inline void render_preparing(FrameBuffer& frame, const DisplaySnapshot& s, const DisplayChanges& changes) {
  if (changes.preparingHeader) {
    frame.fillRect(24, 48, 320, 24, TFT_BLACK);
    frame.setFont(&fonts::Font0);
    frame.setTextSize(1);
    frame.setTextColor(TFT_WHITE, TFT_BLACK);
    frame.drawString(preparing_state(s) == PreparingState::Waiting ? preparing_wait_label(s) :
                     s.mode == ControlMode::Manual ? "MANUAL" : "AUTO", 24, 56);
  }
  if (changes.preparingBody) draw_preparing_body(frame, s);
  else {
    if (changes.preparingBar) draw_preparing_bar(frame, s);
    if (changes.preparingArrows) draw_preparing_arrows(frame, s);
  }
}

#endif // PREPARING_RENDERER_HPP_
