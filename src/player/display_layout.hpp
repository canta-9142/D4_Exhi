#ifndef DISPLAY_LAYOUT_HPP_
#define DISPLAY_LAYOUT_HPP_

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

// 結果③のベストスコアの3行。ボタン枠（y=240〜）より上に収める。
constexpr int BEST_ROW_X = 16, BEST_ROW_Y = 84, BEST_ROW_PITCH = 52, BEST_ROW_WIDTH = 448, BEST_ROW_HEIGHT = 48;

#endif // DISPLAY_LAYOUT_HPP_
