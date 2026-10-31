#ifndef IDLE_RENDERER_HPP_
#define IDLE_RENDERER_HPP_

#include "frame_buffer.hpp"
#include "display_snapshot.hpp"

// 待機の長押しのバー。押している間は2秒で満ち、readyの確認待ちでは満タンのままにする。
inline void draw_idle_bar(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(IDLE_BAR_X, IDLE_BAR_Y, IDLE_BAR_WIDTH, IDLE_BAR_HEIGHT, TFT_BLACK);
  if (!idle_guide(s)) return;
  frame.drawRect(IDLE_BAR_X, IDLE_BAR_Y, IDLE_BAR_WIDTH, IDLE_BAR_HEIGHT, TFT_WHITE);
  const unsigned progress = lobby_state(s) == LobbyState::Confirming ?
      ReadyHold::DURATION_MS : std::min<unsigned>(s.readyHoldMs, ReadyHold::DURATION_MS);
  const int width = progress * (IDLE_BAR_WIDTH - 4) / ReadyHold::DURATION_MS;
  if (width) frame.fillRect(IDLE_BAR_X + 2, IDLE_BAR_Y + 2, width, IDLE_BAR_HEIGHT - 4, TFT_WHITE);
}

// 右の情報欄の、状態より下を描き直す。長押しの案内を出さない状態では空にする。
inline void draw_idle_guide(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(INFO_X, IDLE_GUIDE_Y, INFO_WIDTH, IDLE_GUIDE_HEIGHT, TFT_BLACK);
  if (!idle_guide(s)) return;
  frame.setFont(&fonts::Font0);
  frame.setTextSize(1);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString("Press and hold", INFO_X + 5, 121);
  frame.drawString("both end buttons.", INFO_X + 5, 133);
  draw_idle_bar(frame, s);
  frame.drawString(lobby_state(s) == LobbyState::Confirming ? "CONFIRMING..." : "Hold 2 seconds.",
                   INFO_X + 5, IDLE_BAR_Y + IDLE_BAR_HEIGHT + 8);
}

// 下向きの矢印（実際のボタンを指す）。押している間は白、押していなければ暗い灰色。
inline void draw_down_arrow(FrameBuffer& frame, int x, int top, int length, uint16_t color) {
  frame.fillRect(x - 2, top, 4, length - 16, color);
  frame.fillTriangle(x - 12, top + length - 16, x + 12, top + length - 16, x, top + length, color);
}

// 両端の枠に、長押しするボタンを指す矢印を描く。ボタンの代わりに、案内の間だけ描く。
inline void draw_idle_arrows(FrameBuffer& frame, const DisplaySnapshot& s) {
  for (unsigned side = 0; side < 2; ++side) {
    const int index = side ? 3 : 0;
    frame.fillRect(button_frame_x(index), BUTTON_FRAME_Y, BUTTON_FRAME_WIDTH, BUTTON_FRAME_HEIGHT, TFT_BLACK);
    if (!idle_guide(s)) continue;
    const int x = button_frame_x(index) + BUTTON_FRAME_WIDTH / 2;
    const uint8_t bit = side ? 8 : 1;
    frame.setFont(&fonts::Font0);
    frame.setTextSize(2);
    frame.setTextColor(TFT_WHITE, TFT_BLACK);
    frame.drawString("HOLD", x - frame.textWidth("HOLD") / 2, 250);
    draw_down_arrow(frame, x, 274, 40, s.pressedMask & bit ? TFT_WHITE : TFT_DARKGREY);
  }
}

#endif // IDLE_RENDERER_HPP_
