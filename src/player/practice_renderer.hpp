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

// 時間欄。練習の残り秒数を常に表示し、待機へ戻るまでの10秒以下は注意色。
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

// ボタンの地の色。押下中は黒地にノードの色の文字（draw_button）。
inline uint16_t button_background(NodeId id, bool pressed) {
  return pressed ? TFT_BLACK : nodeVisuals[static_cast<int>(id)].color;
}

// Botのボタンの上端のヒントの帯。地の色で消してから、A〜Hの8枠を描く。ボタンの文字（y=256〜）には掛からない。
inline void draw_hint_band(FrameBuffer& frame, const HintBand& band, int button, uint16_t background) {
  frame.fillRect(hint_band_x(button), HINT_BAND_Y, HINT_BAND_WIDTH, HINT_CELL_HEIGHT, background);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(1);
  for (int cell = 0; cell < HINT_CELLS; ++cell) {
    const NodeVisual& visual = nodeVisuals[static_cast<int>(hint_cell_node(cell))];
    const int x = hint_cell_x(button, cell);
    uint16_t ink;
    switch (band.cells[cell]) {
      case HintCell::Lit: // ノードの色で塗り、黒い太枠と黒い文字。
        frame.fillRect(x, HINT_BAND_Y, HINT_CELL_WIDTH, HINT_CELL_HEIGHT, visual.color);
        for (int b = 0; b < HINT_LIT_BORDER; ++b)
          frame.drawRect(x + b, HINT_BAND_Y + b, HINT_CELL_WIDTH - 2 * b, HINT_CELL_HEIGHT - 2 * b, HINT_LIT_INK);
        ink = HINT_LIT_INK;
        break;
      case HintCell::Linked: // ノードの色を暗くして塗る。
        frame.fillRect(x, HINT_BAND_Y, HINT_CELL_WIDTH, HINT_CELL_HEIGHT, hint_linked_color(visual.color));
        ink = HINT_LINKED_INK;
        break;
      default: // 薄い枠線と文字だけ。
        ink = hint_faint_color(background);
        frame.drawRect(x, HINT_BAND_Y, HINT_CELL_WIDTH, HINT_CELL_HEIGHT, ink);
        break;
    }
    frame.setTextColor(ink); // 背景は透過する。
    frame.drawChar(visual.name, x + HINT_TEXT_DX, HINT_BAND_Y + HINT_TEXT_DY);
  }
}

// Botのボタンの上端の混雑のバーと、満杯のときの文字の下のFULLの札。出さないときは地の色で消す。
inline void draw_bot_bar(FrameBuffer& frame, int load, int button, uint16_t background) {
  frame.fillRect(button_frame_x(button), BOT_BAR_Y, BUTTON_FRAME_WIDTH, BOT_BAR_HEIGHT,
                 load < 0 ? background : bot_load_color(load));
  const int x = bot_full_x(button);
  if (load < 0 || !bot_load_full(load)) {
    frame.fillRect(x, BOT_FULL_Y, BOT_FULL_WIDTH, BOT_FULL_HEIGHT, background);
    return;
  }
  frame.fillRect(x, BOT_FULL_Y, BOT_FULL_WIDTH, BOT_FULL_HEIGHT, BOT_LOAD_RED);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(BOT_FULL_TEXT_SIZE);
  frame.setTextColor(BOT_FULL_INK);
  const int width = frame.textWidth("FULL") - BOT_FULL_TEXT_SIZE; // 右端の1列は空き。
  frame.drawString("FULL", x + (BOT_FULL_WIDTH - width) / 2, BOT_FULL_Y + (BOT_FULL_HEIGHT - 7 * BOT_FULL_TEXT_SIZE) / 2);
}

// 練習の帯とバー。ボタンの後に描く（ボタンの塗りが帯とバーを消すため）。
inline void draw_practice_hints(FrameBuffer& frame, const DisplaySnapshot& s, const DisplayChanges& changes) {
  for (int slot = 0; slot < HINT_BUTTONS; ++slot) {
    const int button = HINT_FIRST_BUTTON + slot;
    const uint8_t bit = 1u << button;
    if (!((changes.hintBands | changes.botBars) & bit)) continue;
    const NodeId bot = bottom_bot(s.selfId, slot);
    if (bot == NodeId::None) continue; // ボタンを描かない（自ノード未定）。
    const uint16_t background = button_background(bot, s.pressedMask & bit);
    if (changes.hintBands & bit) draw_hint_band(frame, displayed_hint(s).bands[slot], button, background);
    if (changes.botBars & bit) draw_bot_bar(frame, displayed_bot_load(s, slot), button, background);
  }
}

#endif // PRACTICE_RENDERER_HPP_
