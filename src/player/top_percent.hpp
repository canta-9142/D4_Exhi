#ifndef TOP_PERCENT_HPP_
#define TOP_PERCENT_HPP_

#include <algorithm>
#include <cmath>
#include <cstdint>

// 上位p%の境界 z = Φ⁻¹(1 - p/100) を1/1000単位で持つ。添字はp - 1。
constexpr int16_t TOP_PERCENT_Z_MILLI[50] = {
  2326, 2054, 1881, 1751, 1645, 1555, 1476, 1405, 1341, 1282,
  1227, 1175, 1126, 1080, 1036, 994, 954, 915, 878, 842,
  806, 772, 739, 706, 674, 643, 613, 583, 553, 524,
  496, 468, 440, 412, 385, 358, 332, 305, 279, 253,
  228, 202, 176, 151, 126, 100, 75, 50, 25, 0,
};

inline int top_percent_z_milli(uint8_t percent) {
  return percent >= 1 && percent <= 50 ? TOP_PERCENT_Z_MILLI[percent - 1] : 0;
}

// ベルカーブは x = -3.5〜3.5 を BELL_WIDTH 列へ割り当て、y = exp(-x²/2) を BELL_HEIGHT 画素で描く。
constexpr int BELL_X = 24, BELL_WIDTH = 280, BELL_BASE = 228, BELL_HEIGHT = 64;
// 曲線・塗り・人のアイコンを含む更新範囲。ボタン枠（y=240〜）より上に収める。
constexpr int BELL_AREA_X = 16, BELL_AREA_Y = 140, BELL_AREA_W = 296, BELL_AREA_H = 92;
// 10×10のマス。左上から行ごとに上位の順で並べる。
constexpr int GRID_X = 355, GRID_Y = 146, GRID_PITCH = 9, GRID_CELL = 8;
constexpr int GRID_SIZE = GRID_PITCH * 9 + GRID_CELL;

inline int bell_height(int column) {
  const float x = -3.5f + 7.0f * (column + 0.5f) / BELL_WIDTH;
  return static_cast<int>(std::lround(BELL_HEIGHT * std::exp(-x * x / 2)));
}

inline int bell_x(int zMilli) { return BELL_X + (zMilli + 3500) * BELL_WIDTH / 7000; }

// 2ページ目の初回表示で、塗りを右端から広げる。終了演出と同じease-out。
inline unsigned top_intro_ease(uint16_t progress) {
  const uint64_t left = 1000 - std::min<unsigned>(progress, 1000);
  return static_cast<unsigned>(1000 - left * left * left / 1000000);
}

// この画面上のX以降の列を自分の色で塗る。
inline int top_fill_x(int zx, uint16_t progress) {
  const int right = BELL_X + BELL_WIDTH;
  return right - static_cast<int>((right - zx) * top_intro_ease(progress) / 1000);
}

inline int top_filled_cells(uint8_t percent, uint16_t progress) {
  return static_cast<int>(percent * top_intro_ease(progress) / 1000);
}

inline int grid_cell_x(int index) { return GRID_X + index % 10 * GRID_PITCH; }
inline int grid_cell_y(int index) { return GRID_Y + index / 10 * GRID_PITCH; }

#endif // TOP_PERCENT_HPP_
