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
  // 右端のボタン枠（x=360〜）にはページ送りの案内を描くため、消去しない。
  frame.fillRect(24, 216, 336, 104, TFT_BLACK);
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

// 右端のボタン枠に、押すと次のページへ進むことと現在のページを示す。上位%がなければ何も出さない。
inline void draw_next_hint(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(361, 241, 119, 79, TFT_BLACK);
  if (!s.summary.topPercent) return;
  frame.setFont(&fonts::Font0);
  frame.setTextSize(2);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString("NEXT", 420 - frame.textWidth("NEXT") / 2, 250);
  frame.fillTriangle(404, 272, 404, 300, 436, 286, TFT_WHITE);
  for (int page = 0; page < 2; ++page) {
    const int x = 412 + page * 16;
    if (page == static_cast<int>(s.resultPage)) frame.fillCircle(x, 310, 3, TFT_WHITE);
    else frame.drawCircle(x, 310, 3, TFT_WHITE);
  }
}

constexpr uint16_t TOP_DIM = 0x39E7;

// 曲線の高さは定数なので、初回の描画で1度だけ求める。描画タスクだけが呼ぶ。
inline const uint8_t* bell_heights() {
  static uint8_t heights[BELL_WIDTH];
  static bool ready = false;
  if (!ready) {
    for (int column = 0; column < BELL_WIDTH; ++column) heights[column] = bell_height(column);
    ready = true;
  }
  return heights;
}

inline void draw_top_curve(FrameBuffer& frame, const DisplaySnapshot& s) {
  const uint16_t color = nodeVisuals[static_cast<int>(s.selfId)].color;
  const int zx = bell_x(top_percent_z_milli(s.summary.topPercent));
  const int fillX = top_fill_x(zx, s.topProgress);
  frame.fillRect(BELL_AREA_X, BELL_AREA_Y, BELL_AREA_W, BELL_AREA_H, TFT_BLACK);
  const uint8_t* heights = bell_heights();
  int previousTop = BELL_BASE - heights[0];
  for (int column = 0; column < BELL_WIDTH; ++column) {
    const int x = BELL_X + column;
    const int top = BELL_BASE - heights[column];
    frame.drawFastVLine(x, top, BELL_BASE - top, x >= fillX ? color : TOP_DIM);
    frame.drawLine(x - 1, previousTop, x, top, TFT_WHITE);
    frame.drawLine(x - 1, previousTop - 1, x, top - 1, TFT_WHITE);
    previousTop = top;
  }
  frame.drawFastHLine(BELL_X, BELL_BASE, BELL_WIDTH, TFT_WHITE);
  // 自分の位置に縦線と人のアイコンを立てる。
  const int top = BELL_BASE - heights[zx - BELL_X];
  frame.drawFastVLine(zx, top - 4, BELL_BASE - top + 4, TFT_WHITE);
  frame.fillTriangle(zx, top - 13, zx - 5, top - 4, zx + 5, top - 4, TFT_WHITE);
  frame.fillCircle(zx, top - 17, 3, TFT_WHITE);
}

inline void draw_top_cell(FrameBuffer& frame, const DisplaySnapshot& s, int index) {
  const uint16_t color = nodeVisuals[static_cast<int>(s.selfId)].color;
  const bool self = index == s.summary.topPercent - 1;
  const uint16_t fill = index >= top_filled_cells(s.summary.topPercent, s.topProgress) ? TOP_DIM :
                        self && s.previewLit ? TFT_WHITE : color;
  frame.fillRect(grid_cell_x(index), grid_cell_y(index), GRID_CELL, GRID_CELL, fill);
}

inline void draw_top_grid(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(GRID_X, GRID_Y, GRID_SIZE, GRID_SIZE, TFT_BLACK);
  for (int index = 0; index < 100; ++index) draw_top_cell(frame, s, index);
}

inline void draw_top_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  char text[32];
  snprintf(text, sizeof(text), "TOP %u%%", unsigned(s.summary.topPercent));
  frame.setFont(&fonts::Font0);
  frame.setTextSize(4);
  frame.setTextColor(nodeVisuals[static_cast<int>(s.selfId)].color, TFT_BLACK);
  frame.drawString(text, (FrameBuffer::WIDTH - frame.textWidth(text)) / 2, 88);
  snprintf(text, sizeof(text), "YOU ARE IN THE TOP %u%%!", unsigned(s.summary.topPercent));
  preparing_text(frame, text, 124);
  draw_top_curve(frame, s);
  draw_top_grid(frame, s);
  draw_next_hint(frame, s);
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
    } else if (s.resultPage == ResultPage::Score) preparing_text(frame, "FINAL SCORE", 96, 2);
  }
  if (state != FinishedState::Ready) return;
  if (s.resultPage == ResultPage::TopPercent) {
    if (changes.topBody) draw_top_body(frame, s);
    else {
      if (changes.topCurve) {
        draw_top_curve(frame, s);
        draw_top_grid(frame, s);
      } else if (changes.topCell) draw_top_cell(frame, s, changes.topCell - 1);
    }
    return;
  }
  if (changes.resultScore) draw_final_score(frame, s);
  if (changes.resultRank) draw_final_rank(frame, s);
  if (changes.resultAccent) draw_result_accent(frame, s);
  if (changes.resultBody) draw_next_hint(frame, s);
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
