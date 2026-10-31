#ifndef DISPLAY_LAYOUT_HPP_
#define DISPLAY_LAYOUT_HPP_

#include <cstdint>

constexpr int ANIMATION_X = 120;
constexpr int ANIMATION_Y = 0;
constexpr int ANIMATION_WIDTH = 200;
constexpr int ANIMATION_HEIGHT = 240;

// Playing画面の演出を描く左の列（キュー位置の数字3・2・1の背景）と、カード欄の右の余白。境界線（x=360、y=240）は含めない。
constexpr int LEFT_COLUMN_X = 1, LEFT_COLUMN_Y = 0, LEFT_COLUMN_WIDTH = 119, LEFT_COLUMN_HEIGHT = 240;
constexpr int RIGHT_MARGIN_X = 320, RIGHT_MARGIN_Y = 0, RIGHT_MARGIN_WIDTH = 40, RIGHT_MARGIN_HEIGHT = 240;

// Playing画面の右の情報欄（x=361〜479、y=81〜239）。上から状態、獲得点が流れる帯、スコア、READY、残り時間、暫定順位。
constexpr int INFO_X = 361, INFO_WIDTH = 118, INFO_HEADER_HEIGHT = 32;
constexpr int GAIN_LANE_Y = 113, GAIN_LANE_HEIGHT = 30;
constexpr int INFO_SCORE_Y = 143, INFO_READY_Y = 170, INFO_REMAINING_Y = 181;
constexpr int INFO_RANK_X = 418, INFO_RANK_Y = 205, INFO_RANK_WIDTH = 60, INFO_RANK_HEIGHT = 34;
// 獲得点は文字サイズ2の「+15」（36×16）を3列に並べ、帯の下端からスコアの上へ流す。
constexpr int GAIN_COLUMN_X = 364, GAIN_COLUMN_PITCH = 39, GAIN_TEXT_HEIGHT = 16;
constexpr int GAIN_START_Y = GAIN_LANE_Y + GAIN_LANE_HEIGHT - GAIN_TEXT_HEIGHT;

// 下部の4つのボタン枠（左から順にx=0,120,240,360。枠線の内側）。
constexpr int BUTTON_FRAME_Y = 241, BUTTON_FRAME_WIDTH = 119, BUTTON_FRAME_HEIGHT = 79;
constexpr int button_frame_x(int index) { return index * 120 + 1; }
// ボタンの文字（ノードの表示名）。区画の左上（x=i*120、y=240）から(42, 16)に、Font0の文字サイズ6で描く。
// 字形は5×7ドットなので、画素では30×42（下部のボタンではy=256〜297）。
constexpr int BUTTON_LETTER_DX = 42, BUTTON_LETTER_DY = 16, BUTTON_LETTER_SIZE = 6;
constexpr int BUTTON_LETTER_WIDTH = 5 * BUTTON_LETTER_SIZE, BUTTON_LETTER_HEIGHT = 7 * BUTTON_LETTER_SIZE;
constexpr int button_letter_x(int index) { return index * 120 + BUTTON_LETTER_DX; }
constexpr int BUTTON_LETTER_Y = 240 + BUTTON_LETTER_DY;

// 練習のヒントの帯。Botのボタン（左から2番目・3番目）の上端、混雑のバーのすぐ下に、A〜Hの8枠を横に並べる。
// 枠は13×11で1画素の隙間を空け、帯の幅は111。帯の下端（y=254）とボタンの文字の上端（y=256）の間に1行空ける。
constexpr int HINT_FIRST_BUTTON = 1, HINT_BUTTONS = 2, HINT_CELLS = 8;
constexpr int HINT_CELL_WIDTH = 13, HINT_CELL_HEIGHT = 11, HINT_CELL_PITCH = 14;
constexpr int HINT_BAND_DX = 4, HINT_BAND_Y = 244;
constexpr int HINT_BAND_WIDTH = (HINT_CELLS - 1) * HINT_CELL_PITCH + HINT_CELL_WIDTH;
constexpr int hint_band_x(int button) { return button_frame_x(button) + HINT_BAND_DX; }
constexpr int hint_cell_x(int button, int cell) { return hint_band_x(button) + cell * HINT_CELL_PITCH; }
// 枠の中の名前（Font0の文字サイズ1、字形5×7）。光る枠の太枠（2画素）に掛からない位置。
constexpr int HINT_LIT_BORDER = 2, HINT_TEXT_DX = 4, HINT_TEXT_DY = 2;
// 光る枠の太枠と文字、つながる枠の文字の色。つながる枠はノードの色をHINT_LINKED_PERCENTの明るさにして塗る。
// つながらない枠の線と文字は、ボタンの地の色をHINT_FAINT_PERCENTの明るさにした色。地が黒（押下中）ならHINT_FAINT_ON_BLACK。
constexpr uint16_t HINT_LIT_INK = 0x0000, HINT_LINKED_INK = 0xFFFF, HINT_FAINT_ON_BLACK = 0x4208;
constexpr int HINT_LINKED_PERCENT = 50, HINT_FAINT_PERCENT = 60;

// 練習のBotの混雑（status.bots）。Botのボタンの上端の細いバーと、満杯（4件）のときに文字の下に出すFULLの札。
constexpr int BOT_BAR_Y = BUTTON_FRAME_Y, BOT_BAR_HEIGHT = 3; // 幅はボタン枠と同じ（BUTTON_FRAME_WIDTH）。
constexpr int BOT_FULL_DX = 33, BOT_FULL_Y = 302, BOT_FULL_WIDTH = 52, BOT_FULL_HEIGHT = 16, BOT_FULL_TEXT_SIZE = 2;
constexpr int bot_full_x(int button) { return button_frame_x(button) + BOT_FULL_DX; }
// シグナルタワー（LED制御のBUFFER）と同じ色。0〜1が青、2が緑、3が黄、4が赤。FULLの札は赤の地に白の文字。
constexpr uint16_t BOT_LOAD_BLUE = 0x001F, BOT_LOAD_GREEN = 0x07E0, BOT_LOAD_YELLOW = 0xFFE0, BOT_LOAD_RED = 0xF800;
constexpr uint16_t BOT_FULL_INK = 0xFFFF;
constexpr uint8_t BOT_LOAD_MAX = 4;

// 練習ラウンド。左上（キュー位置の数字3の上）のTUTORIALの札。READYの位置にPRACTICE、残り時間の位置にTIMEを出す。
constexpr int PRACTICE_TAG_X = 4, PRACTICE_TAG_Y = 2, PRACTICE_TAG_WIDTH = 112, PRACTICE_TAG_HEIGHT = 22;

// 見出しの隅に出す、段階の残り秒数（モード選択・結果）。10秒以下は注意色。結果②のヘッダーの絵（x=200〜）と重ねない。
constexpr int HEADER_WINDOW_X = 144, HEADER_WINDOW_Y = 20, HEADER_WINDOW_WIDTH = 56, HEADER_WINDOW_HEIGHT = 24;
constexpr int WINDOW_WARNING_SECONDS = 10;

// 待機の右の情報欄。状態の下から枠の上端までに、長押しの案内と2秒で満ちるバーを出す。
constexpr int IDLE_GUIDE_Y = 113, IDLE_GUIDE_HEIGHT = 127;
constexpr int IDLE_BAR_X = 364, IDLE_BAR_Y = 152, IDLE_BAR_WIDTH = 112, IDLE_BAR_HEIGHT = 14;

// 結果①の順位（対象者のRANK nと対象外のNOT RANKED）。ボタン枠（y=241〜）に掛けない。
constexpr int RESULT_RANK_X = 24, RESULT_RANK_Y = 200, RESULT_RANK_WIDTH = 336, RESULT_RANK_HEIGHT = 40;

// 結果③のベストスコアの3行。ボタン枠（y=240〜）より上に収める。
constexpr int BEST_ROW_X = 16, BEST_ROW_Y = 84, BEST_ROW_PITCH = 52, BEST_ROW_WIDTH = 448, BEST_ROW_HEIGHT = 48;

#endif // DISPLAY_LAYOUT_HPP_
