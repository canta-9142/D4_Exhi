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

// 右端のボタン枠に、押すと次のページへ進むことと現在のページを示す。英語が読めなくても分かるよう矢印を大きくする。
inline void draw_next_hint(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(361, 241, 119, 79, TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(2);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString("NEXT", 374, 264);
  frame.fillTriangle(430, 254, 430, 290, 466, 272, TFT_WHITE);
  const int count = result_page_count(s.summary);
  const int current = result_page_index(s.summary, s.resultPage);
  for (int page = 0; page < count; ++page) {
    const int x = 420 - (count - 1) * 8 + page * 16;
    if (page == current) frame.fillCircle(x, 306, 4, TFT_WHITE);
    else frame.drawCircle(x, 306, 4, TFT_WHITE);
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

// ベルカーブをleftから描き、fillFrom〜fillTo（列番号、終端を含まない）を自ノードの色、残りを暗い灰色で塗る。
inline void draw_bell(FrameBuffer& frame, int left, int fillFrom, int fillTo, uint16_t color) {
  const uint8_t* heights = bell_heights();
  int previousTop = BELL_BASE - heights[0];
  for (int column = 0; column < BELL_WIDTH; ++column) {
    const int x = left + column;
    const int top = BELL_BASE - heights[column];
    frame.drawFastVLine(x, top, BELL_BASE - top, column >= fillFrom && column < fillTo ? color : TOP_DIM);
    frame.drawLine(x - 1, previousTop, x, top, TFT_WHITE);
    frame.drawLine(x - 1, previousTop - 1, x, top - 1, TFT_WHITE);
    previousTop = top;
  }
  frame.drawFastHLine(left, BELL_BASE, BELL_WIDTH, TFT_WHITE);
}

inline void draw_top_curve(FrameBuffer& frame, const DisplaySnapshot& s) {
  const uint16_t color = nodeVisuals[static_cast<int>(s.selfId)].color;
  const int zx = bell_x(top_percent_z_milli(s.summary.topPercent));
  const int fillX = top_fill_x(zx, s.topProgress);
  frame.fillRect(BELL_AREA_X, BELL_AREA_Y, BELL_AREA_W, BELL_AREA_H, TFT_BLACK);
  draw_bell(frame, BELL_X, fillX - BELL_X, BELL_WIDTH, color);
  // 自分の位置に縦線と人のアイコンを立てる。
  const int top = BELL_BASE - bell_heights()[zx - BELL_X];
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

// 下位50%は値を送られないので、文字とベルカーブの左半分だけを中央に描く。位置の印・マス・演出はない。
inline void draw_bottom_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  const uint16_t color = nodeVisuals[static_cast<int>(s.selfId)].color;
  frame.setFont(&fonts::Font0);
  frame.setTextSize(4);
  frame.setTextColor(color, TFT_BLACK);
  frame.drawString("BOTTOM 50%", (FrameBuffer::WIDTH - frame.textWidth("BOTTOM 50%")) / 2, 88);
  draw_bell(frame, (FrameBuffer::WIDTH - BELL_WIDTH) / 2, 0, BELL_WIDTH / 2, color);
  draw_next_hint(frame, s);
}

inline void draw_top_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  if (!s.summary.topPercent) {
    draw_bottom_body(frame, s);
    return;
  }
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

// ③の行頭の絵。8×8のドット絵を3倍で描く。各行の最上位ビットが左端。
constexpr uint8_t BEST_ICONS[3][8] = {
  {0xFE, 0xFC, 0xFE, 0xFC, 0x80, 0x80, 0x80, 0xE0}, // 旗（その回）
  {0x3C, 0x42, 0x91, 0x91, 0x9D, 0x81, 0x42, 0x3C}, // 時計（時間帯）
  {0x99, 0x99, 0xDB, 0xFF, 0xFF, 0xFF, 0x00, 0xFF}, // 王冠（通算）
};

inline void draw_best_row(FrameBuffer& frame, const DisplaySnapshot& s, int row, const char* label,
                          int32_t best, bool mine, uint8_t nodes) {
  const uint16_t self = nodeVisuals[static_cast<int>(s.selfId)].color;
  const uint16_t accent = mine ? self : TFT_WHITE;
  const int x = BEST_ROW_X, y = BEST_ROW_Y + row * BEST_ROW_PITCH;
  // 自分のベストは行を自ノードの色の太枠で囲み、YOU!を出す。
  frame.drawRect(x, y, BEST_ROW_WIDTH, BEST_ROW_HEIGHT, mine ? self : TOP_DIM);
  if (mine) frame.drawRect(x + 1, y + 1, BEST_ROW_WIDTH - 2, BEST_ROW_HEIGHT - 2, self);
  for (int line = 0; line < 8; ++line)
    for (int column = 0; column < 8; ++column)
      if (BEST_ICONS[row][line] & (0x80u >> column)) frame.fillRect(x + 10 + column * 3, y + 12 + line * 3, 3, 3, accent);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(2);
  frame.setTextColor(accent, TFT_BLACK);
  frame.drawString(label, x + 44, y + 4);
  if (mine) frame.drawString("YOU!", x + BEST_ROW_WIDTH - 10 - frame.textWidth("YOU!"), y + 4);
  char text[12] = "---";
  if (best != FinalSummary::NO_BEST) snprintf(text, sizeof(text), "%ld", static_cast<long>(best));
  frame.setTextSize(3);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString(text, x + 44, y + 22);
  // その回のベストを取ったノードを、ノードの色の札で並べる。
  int tagX = x + 44 + frame.textWidth(text) + 12;
  frame.setTextSize(2);
  for (int node = 1; node <= 8; ++node) {
    if (!(nodes & (1u << (node - 1)))) continue;
    const auto& visual = nodeVisuals[node];
    frame.fillRect(tagX, y + 22, 18, 24, visual.color);
    frame.setTextColor(TFT_BLACK, visual.color);
    frame.drawChar(visual.name, tagX + 3, y + 26);
    tagX += 22;
  }
}

inline void draw_best_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  const FinalSummary& r = s.summary;
  const unsigned self = static_cast<unsigned>(s.selfId);
  const bool gameMine = self >= 1 && self <= 8 && (r.gameBestNodes & (1u << (self - 1)));
  char hour[16] = "THIS HOUR";
  if (r.hourBestStart >= 0) snprintf(hour, sizeof(hour), "%02d:00-%02d:59", r.hourBestStart, r.hourBestStart);
  draw_best_row(frame, s, 0, "THIS GAME", r.gameBest, gameMine, r.gameBestNodes);
  draw_best_row(frame, s, 1, hour, r.hourBest, r.hourBestMine, 0);
  draw_best_row(frame, s, 2, "ALL TIME", r.allTimeBest, r.allTimeBestMine, 0);
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
  if (s.resultPage == ResultPage::Bests) {
    if (changes.resultBody) draw_best_body(frame, s);
    return;
  }
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
