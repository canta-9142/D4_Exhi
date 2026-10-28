#ifndef FINISHED_RENDERER_HPP_
#define FINISHED_RENDERER_HPP_

#include "preparing_renderer.hpp"
#include "node_config.hpp"

inline void draw_result_layout(FrameBuffer& frame, bool preview) {
  frame.fillScreen(TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(3);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString(preview ? "IDENTIFY" : "RESULT", 24, 20);
  frame.drawFastHLine(0, 80, 480, TFT_WHITE);
}

inline void draw_final_score(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(24, 128, 432, 56, TFT_BLACK);
  char text[12];
  snprintf(text, sizeof(text), "%ld", static_cast<long>(displayed_final_score(s)));
  preparing_text(frame, text, 128, 6);
}

inline void draw_final_rank(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(24, 216, 432, 104, TFT_BLACK);
  if (s.finishedProgress < 500) return;
  const unsigned progress = std::min<unsigned>(s.finishedProgress, 1000);
  const int offset = (1000 - progress) * 24 / 500;
  if (!s.summary.eligible) {
    preparing_text(frame, "NOT RANKED", 240 + offset, 3);
  } else {
    char text[12];
    snprintf(text, sizeof(text), "RANK %u", unsigned(s.summary.rank));
    preparing_text(frame, text, 228 + offset, 6);
  }
}

inline void draw_result_accent(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(40, 192, 400, 4, TFT_BLACK);
  const int width = std::min<unsigned>(s.finishedProgress, 1000) * 400 / 1000;
  if (width) frame.fillRect(40, 192, width, 4, nodeVisuals[static_cast<int>(s.selfId)].color);
}

inline void render_finished(FrameBuffer& frame, const DisplaySnapshot& s, const DisplayChanges& changes) {
  const auto state = finished_state(s);
  if (changes.resultHeader) {
    frame.fillRect(24, 48, 320, 24, TFT_BLACK);
    frame.setFont(&fonts::Font0);
    frame.setTextSize(1);
    frame.setTextColor(TFT_WHITE, TFT_BLACK);
    frame.drawString(s.mode == ControlMode::Manual ? "MANUAL" : "AUTO", 24, 56);
  }
  if (changes.resultBody) {
    frame.fillRect(0, 81, 480, 239, TFT_BLACK);
    if (state != FinishedState::Ready) {
      preparing_text(frame, state == FinishedState::Syncing ? "SYNCING" :
          state == FinishedState::Status ? "WAIT STATUS" : "WAIT RESULT", 152, 3);
      preparing_text(frame, "Please wait.", 208);
    } else preparing_text(frame, "FINAL SCORE", 96, 2);
  }
  if (state != FinishedState::Ready) return;
  if (changes.resultScore) draw_final_score(frame, s);
  if (changes.resultRank) draw_final_rank(frame, s);
  if (changes.resultAccent) draw_result_accent(frame, s);
}

inline void render_preview(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(0, 81, 480, 239, TFT_BLACK);
  if (!s.previewLit) return;
  const auto& visual = nodeVisuals[static_cast<int>(s.selfId)];
  frame.setFont(&fonts::Font0);
  frame.setTextSize(18);
  frame.setTextColor(visual.color, TFT_BLACK);
  const char text[] = {visual.name, '\0'};
  frame.drawString(text, (480 - frame.textWidth(text)) / 2, 112);
}

#endif // FINISHED_RENDERER_HPP_
