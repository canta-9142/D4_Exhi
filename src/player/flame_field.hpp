#ifndef FLAME_FIELD_HPP_
#define FLAME_FIELD_HPP_

#include <algorithm>
#include <cstdint>
#include <cstring>
#include "display_layout.hpp"
#include "effect_random.hpp"
#include "pixel_art.hpp"

// 炎は4×4画素を1マスとする粗い格子で描く。左の列（x=1〜119）は30×60マスを x=0 から置き、x=0 は描かない。
// カード欄の右の余白（x=320〜359）は10×60マス。境界線（x=360、y=240）には描かない。
constexpr int FLAME_CELL = 4;
constexpr int FLAME_ROWS = 60;
constexpr int FLAME_LEFT_COLUMNS = 30, FLAME_RIGHT_COLUMNS = 10;
constexpr int FLAME_LEFT_X = 0, FLAME_RIGHT_X = RIGHT_MARGIN_X;
constexpr uint32_t FLAME_LEFT_SEED = 11, FLAME_RIGHT_SEED = 23;

// 炎の進む間隔（約15fps）。重ければ10fps（100ms）へ下げる。
constexpr uint32_t FLAME_STEP_MS = 66;

// 熱は1/4マス単位。1行上へ伝えるたびに0〜2マス（平均1マス）減らすので、炎の高さはおよそ火種の熱のマス数になる。
constexpr uint8_t FLAME_HEAT_PER_ROW = 4;
constexpr uint8_t FLAME_MAX_DECAY = 8;

// 黒 → 暗い赤 → 赤 → 暗い橙の8段階。最も明るい色でもフル輝度の約35%（各成分0x59まで）に抑える。
constexpr uint8_t FLAME_LEVELS = 8;
constexpr uint8_t FLAME_MAX_BRIGHTNESS = 0x59;
constexpr uint32_t FLAME_PALETTE_RGB[FLAME_LEVELS] = {
  0x000000, 0x1a0300, 0x300600, 0x460c00, 0x551800, 0x592400, 0x593000, 0x593a08,
};
// コンボ20以上では1段明るくする。上限は同じ。
constexpr uint32_t FLAME_HOT_PALETTE_RGB[FLAME_LEVELS] = {
  0x000000, 0x300600, 0x460c00, 0x551800, 0x592400, 0x593000, 0x593a08, 0x594612,
};
// 火の粉と煙も炎の一部として同じ上限に収める。
constexpr uint32_t FLAME_SPARK_RGB = 0x594010;
constexpr uint32_t SMOKE_RGB[2] = {0x5a5a5a, 0x3c3c3c};

inline uint16_t flame_color(uint8_t level, bool hot) {
  return art_rgb((hot ? FLAME_HOT_PALETTE_RGB : FLAME_PALETTE_RGB)[std::min<uint8_t>(level, FLAME_LEVELS - 1)]);
}

// マスの熱をパレットの段階（0〜7）へ。scaleで正規化し、低い炎でも根元を明るくする。
inline uint8_t flame_level(uint8_t heat, uint8_t scale) {
  if (heat <= 1) return 0;
  const unsigned top = std::max<unsigned>(scale, FLAME_HEAT_PER_ROW);
  return static_cast<uint8_t>(std::min<unsigned>(FLAME_LEVELS - 1, (heat * (FLAME_LEVELS - 1) * 2 + top) / (2 * top)));
}

// 昔ながらの炎。下の行に熱を置き、上の行へ左右1マス揺らしながら伝えて少しずつ減らす。
// 熱の格子は描画側だけが持ち、スナップショットの進行の番号まで進める。LCD・FreeRTOSには依存しない。
template <int Columns>
class FlameField {
public:
  explicit FlameField(uint32_t seed) : seed_(seed), rng_(seed) { std::memset(heat_, 0, sizeof(heat_)); }

  void reset() { clear(0); }

  // 熱を消し、進行の番号をstepに合わせる。演出をやり直したときに使う。
  void clear(uint32_t step) {
    std::memset(heat_, 0, sizeof(heat_));
    rng_ = Xorshift32(seed_);
    step_ = step;
  }

  // fuelは火種の熱（1/4マス単位）。0なら火種を止め、残りの熱は FLAME_ROWS 段以内に消える。
  void step(uint8_t fuel) {
    for (int x = 0; x < Columns; ++x)
      heat_[FLAME_ROWS - 1][x] = fuel ? static_cast<uint8_t>(std::min<unsigned>(255, fuel * (218 + rng_.below(77)) / 256)) : 0;
    for (int y = 0; y < FLAME_ROWS - 1; ++y) {
      for (int x = 0; x < Columns; ++x) {
        const int source = std::min(Columns - 1, std::max(0, x + int(rng_.below(3)) - 1));
        const int decay = int(rng_.below(FLAME_MAX_DECAY + 1));
        heat_[y][x] = static_cast<uint8_t>(std::max(0, int(heat_[y + 1][source]) - decay));
      }
    }
    ++step_;
  }

  // スナップショットの進行の番号まで進める。描画が遅れて飛んだ段は最新の火種で補う。
  // 番号が戻ったとき（演出のやり直し）と、離れすぎたときは消してから、炎が上端に届く段数だけ進める。
  void advance_to(uint32_t target, uint8_t fuel) {
    if (target < step_ || target - step_ > uint32_t(FLAME_ROWS)) {
      std::memset(heat_, 0, sizeof(heat_));
      rng_ = Xorshift32(seed_ ^ target);
      step_ = target - std::min<uint32_t>(target, FLAME_ROWS);
    }
    while (step_ < target) step(fuel);
  }

  uint32_t steps() const { return step_; }
  uint8_t heat(int x, int y) const { return heat_[y][x]; }
  bool cold() const {
    for (const auto& row : heat_)
      for (uint8_t cell : row)
        if (cell) return false;
    return true;
  }

private:
  uint32_t seed_;
  Xorshift32 rng_;
  uint32_t step_ = 0;
  uint8_t heat_[FLAME_ROWS][Columns];
};

// 段階が0でないマスを、同じ段階が横に続く範囲ごとに呼ぶ。leftは格子の左端の画素。process(x, y, w, h, level)。
template <int Columns, typename F>
void for_each_flame_run(const FlameField<Columns>& field, uint8_t scale, int left, F process) {
  for (int row = 0; row < FLAME_ROWS; ++row) {
    for (int column = 0; column < Columns;) {
      const uint8_t level = flame_level(field.heat(column, row), scale);
      int run = 1;
      while (column + run < Columns && flame_level(field.heat(column + run, row), scale) == level) ++run;
      if (level) process(left + column * FLAME_CELL, row * FLAME_CELL, run * FLAME_CELL, FLAME_CELL, level);
      column += run;
    }
  }
}

// 火の粉: 1マスの明るい点が根元から上へ昇って消える。状態を持たず、出始めからの段数とシードで位置を決める。
struct FlameSpark {
  int8_t column;
  int8_t row;
};
constexpr uint16_t FLAME_SPARK_LIFE = 64;
// 出始めからage段目に、index段目に生まれた火の粉の位置。見えなければfalse。topRowより上には昇らない。
inline bool flame_spark(uint32_t seed, int columns, uint32_t index, uint32_t age, int topRow, FlameSpark& spark) {
  if (index > age || age - index > FLAME_SPARK_LIFE) return false;
  const uint32_t h = effect_hash(seed, index, 1);
  if (h % 2) return false; // 段ごとに半分の確率で生まれる。
  const int speed = 3 + int(h >> 8) % 4; // 1段に0.75〜1.5マス。
  const int row = FLAME_ROWS - 2 - int((age - index) * speed / 4);
  if (row < std::max(0, topRow)) return false;
  spark = {static_cast<int8_t>((h >> 16) % columns), static_cast<int8_t>(row)};
  return true;
}

// 煙: 灰色のマスが上がりながら薄れて消える。炎とマスコットで同じ処理を使う。
constexpr uint16_t SMOKE_MS = 500;
constexpr uint8_t SMOKE_LEVELS = 8;
// 経過時間からの明るさ（0〜SMOKE_LEVELS）。0は消えた。
inline uint8_t smoke_level(uint16_t ms) {
  if (ms >= SMOKE_MS) return 0;
  return static_cast<uint8_t>((uint32_t(SMOKE_MS - ms) * SMOKE_LEVELS + SMOKE_MS - 1) / SMOKE_MS);
}
inline uint16_t smoke_color(uint8_t index, uint8_t level) {
  const uint32_t rgb = SMOKE_RGB[index % 2];
  const auto dim = [&](int shift) { return ((rgb >> shift) & 0xff) * level / SMOKE_LEVELS; };
  return art_rgb(dim(16) << 16 | dim(8) << 8 | dim(0));
}
// 煙のマス（index番目、全count個）の、出どころからのずれ。spreadは消えるまでに広がる半径、riseは上がる画素数。
struct SmokePuff {
  int16_t dx, dy;
};
inline SmokePuff smoke_puff(uint32_t seed, uint8_t index, uint8_t count, uint16_t ms, int spread, int rise) {
  // 円周に並べた方向へ広げる。三角関数を使わず、8方向の表から選ぶ。
  static constexpr int8_t DIRECTIONS[8][2] = {{8, 0}, {6, -6}, {0, -8}, {-6, -6}, {-8, 0}, {-6, 6}, {0, 8}, {6, 6}};
  const uint32_t h = effect_hash(seed, index, 2);
  const auto& d = DIRECTIONS[(index * 8 / std::max<uint8_t>(count, 1) + h % 2) % 8];
  const int k = std::min<int>(ms, SMOKE_MS);
  const int radius = 3 + spread * k / SMOKE_MS;
  return {static_cast<int16_t>(d[0] * radius / 8), static_cast<int16_t>(d[1] * radius / 8 - rise * k / SMOKE_MS)};
}

#endif // FLAME_FIELD_HPP_
