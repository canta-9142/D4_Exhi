#ifndef FRAME_DAMAGE_HPP_
#define FRAME_DAMAGE_HPP_

#include <array>
#include <algorithm>
#include <cassert>
#include <cstdint>
#include <cstring>

// 画面全体のRGB565を比較する。各列の下位20bitが変更された16x16タイル。
class FrameDamage {
public:
  static constexpr int WIDTH = 480;
  static constexpr int HEIGHT = 320;
  static constexpr int TILE = 16;
  static constexpr int COLUMNS = WIDTH / TILE;
  static constexpr int ROWS = HEIGHT / TILE;

  void compare(const void* previous, const void* next) {
    rows_.fill(0);
    count_ = 0;
    const auto* oldPixels = static_cast<const uint8_t*>(previous);
    const auto* newPixels = static_cast<const uint8_t*>(next);
    for (int y = 0; y < HEIGHT; ++y) {
      const uint32_t bit = 1u << (y / TILE);
      for (int column = 0; column < COLUMNS; ++column) {
        if (rows_[column] & bit) continue;
        const size_t offset = (y * WIDTH + column * TILE) * sizeof(uint16_t);
        if (std::memcmp(oldPixels + offset, newPixels + offset, TILE * sizeof(uint16_t))) {
          rows_[column] |= bit;
          ++count_;
        }
      }
    }
  }

  void full() {
    rows_.fill((1u << ROWS) - 1);
    count_ = COLUMNS * ROWS;
  }

  unsigned changedTiles() const { return count_; }

  void markRect(int x, int y, int w, int h) {
    if (w <= 0 || h <= 0) return;
    const int right = std::min(WIDTH, x + w);
    const int bottom = std::min(HEIGHT, y + h);
    x = std::max(0, x);
    y = std::max(0, y);
    if (x >= right || y >= bottom) return;
    for (int column = x / TILE; column <= (right - 1) / TILE; ++column) {
      for (int row = y / TILE; row <= (bottom - 1) / TILE; ++row) {
        const uint32_t bit = 1u << row;
        if (rows_[column] & bit) continue;
        rows_[column] |= bit;
        ++count_;
      }
    }
  }

  bool containsPixel(int x, int y) const {
    assert(x >= 0 && x < WIDTH && y >= 0 && y < HEIGHT);
    return rows_[x / TILE] & (1u << (y / TILE));
  }

  template <typename F>
  void forEachSpan(F process) const {
    for (int column = 0; column < COLUMNS; ++column) {
      for (int row = 0; row < ROWS;) {
        if (!(rows_[column] & (1u << row))) { ++row; continue; }
        const int top = row;
        while (row < ROWS && (rows_[column] & (1u << row))) ++row;
        process(column * TILE, top * TILE, TILE, (row - top) * TILE);
      }
    }
  }

private:
  std::array<uint32_t, COLUMNS> rows_{};
  unsigned count_ = 0;
};

#endif // FRAME_DAMAGE_HPP_
