#ifndef PRACTICE_RENDERER_HPP_
#define PRACTICE_RENDERER_HPP_

#include "preparing_renderer.hpp"

// 左上（キュー位置の数字3の上）の、練習ラウンドであることを示す札。
inline void draw_practice_tag(FrameBuffer& frame) {
  frame.fillRect(PRACTICE_TAG_X, PRACTICE_TAG_Y, PRACTICE_TAG_WIDTH, PRACTICE_TAG_HEIGHT, TUTORIAL_COLOR);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(2);
  frame.setTextColor(TFT_BLACK, TUTORIAL_COLOR);
  frame.drawString("TUTORIAL", PRACTICE_TAG_X + (PRACTICE_TAG_WIDTH - frame.textWidth("TUTORIAL")) / 2,
                   PRACTICE_TAG_Y + (PRACTICE_TAG_HEIGHT - 16) / 2);
}

// 情報欄のREADYの位置に、練習中であることを緑で出す。
inline void draw_practice_label(FrameBuffer& frame) {
  frame.fillRect(366, INFO_READY_Y, 108, 8, TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(1);
  frame.setTextColor(TUTORIAL_COLOR, TFT_BLACK);
  frame.drawString("PRACTICE", 366, INFO_READY_Y);
}

// 時間欄。秒数を出さない間は--。無操作で待機へ戻るまでの10秒以下は注意色。
inline void draw_practice_time(FrameBuffer& frame, const DisplaySnapshot& s) {
  char text[12] = "--";
  const int seconds = practice_time(s.status);
  if (seconds >= 0) snprintf(text, sizeof(text), "%d", seconds);
  frame.fillRect(366, INFO_REMAINING_Y, 72, 24, TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(3);
  frame.setTextColor(practice_time_warn(s.status) ? TFT_ORANGE : TFT_WHITE, TFT_BLACK);
  frame.drawString(text, 438 - frame.textWidth(text), INFO_REMAINING_Y);
}

#endif // PRACTICE_RENDERER_HPP_
