#ifndef COUNTDOWN_RENDERER_HPP_
#define COUNTDOWN_RENDERER_HPP_

#include "frame_buffer.hpp"
#include "display_snapshot.hpp"
#include "node_config.hpp"

// 数字はstatus.countdownをそのまま描く。0の間はGO!を描く。
inline void render_countdown(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(0, 81, 480, 239, TFT_BLACK);
  const char digit[] = {char('0' + s.status.countdown), '\0'};
  const char* text = s.status.countdown ? digit : "GO!";
  frame.setFont(&fonts::Font0);
  frame.setTextSize(18);
  frame.setTextColor(nodeVisuals[static_cast<int>(s.selfId)].color, TFT_BLACK);
  frame.drawString(text, (FrameBuffer::WIDTH - frame.textWidth(text)) / 2, 88);
}

#endif // COUNTDOWN_RENDERER_HPP_
