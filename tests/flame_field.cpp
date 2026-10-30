#include <cassert>
#include <cstdio>
#include <cstring>
#include "../src/player/playing_effects.hpp"

// 炎の高さ: 根元から数えて、段階が0でないマスがある最も高い行の数。
template <int Columns>
static int flame_height(const FlameField<Columns>& field, uint8_t scale) {
  for (int row = 0; row < FLAME_ROWS; ++row)
    for (int x = 0; x < Columns; ++x)
      if (flame_level(field.heat(x, row), scale)) return FLAME_ROWS - row;
  return 0;
}

static bool within_flame_limit(uint16_t color) {
  // RGB565の各成分がフル輝度の約35%（8bitで0x59）以下。
  return (color >> 11) <= (FLAME_MAX_BRIGHTNESS >> 3) && ((color >> 5) & 0x3f) <= (FLAME_MAX_BRIGHTNESS >> 2) &&
         (color & 0x1f) <= (FLAME_MAX_BRIGHTNESS >> 3);
}

int main() {
  // 同じシードと火種なら同じ格子になり、シードが違えば変わる。
  FlameField<FLAME_LEFT_COLUMNS> a(FLAME_LEFT_SEED), b(FLAME_LEFT_SEED), c(FLAME_RIGHT_SEED);
  for (int i = 0; i < 90; ++i) {
    const uint8_t fuel = uint8_t(i < 60 ? 40 + i : 0);
    a.step(fuel);
    b.step(fuel);
    c.step(fuel);
  }
  bool differs = false;
  for (int y = 0; y < FLAME_ROWS; ++y)
    for (int x = 0; x < FLAME_LEFT_COLUMNS; ++x) {
      assert(a.heat(x, y) == b.heat(x, y));
      differs |= a.heat(x, y) != c.heat(x, y);
    }
  assert(differs && a.steps() == 90);
  // 進行の番号まで進めても同じ結果になる。clearで番号を合わせて消せる。
  FlameField<FLAME_LEFT_COLUMNS> caught(FLAME_LEFT_SEED);
  caught.advance_to(20, 40);
  FlameField<FLAME_LEFT_COLUMNS> stepped(FLAME_LEFT_SEED);
  for (int i = 0; i < 20; ++i) stepped.step(40);
  for (int y = 0; y < FLAME_ROWS; ++y)
    for (int x = 0; x < FLAME_LEFT_COLUMNS; ++x) assert(caught.heat(x, y) == stepped.heat(x, y));
  caught.clear(500);
  assert(caught.cold() && caught.steps() == 500);
  caught.advance_to(501, 40); // 番号が続いていれば1段だけ進める（一気に育てない）。
  assert(caught.steps() == 501 && flame_height(caught, 40) <= 2);
  caught.advance_to(10, 40); // 番号が戻ったらやり直す。
  assert(caught.steps() == 10);

  // 火種を止めると、FLAME_ROWS段以内に格子はすべて0になる。
  for (int i = 0; i < FLAME_ROWS; ++i) a.step(0);
  assert(a.cold());

  // combo 0 では炎を出さない。火種も熱もなく、炎の段も進まない（描き直さない）。
  PlayingEffects idle;
  for (uint32_t ms = 0; ms < 10000; ms += 16) {
    idle.update(0, 0, true, ms);
    const EffectsFrame& f = idle.frame();
    assert(!f.flameLit && !f.flameFuel && f.flameStep == 0 && f.smokeMs == EFFECT_OFF && !f.sparks);
  }

  // 段が上がると、炎の高さが単調に増える。火種の高さは段ごとに約0.5秒で追従し、途中で下がらない。
  FlameDriver driver;
  int previousHeight = 0;
  int previousFlame = 0;
  for (uint16_t combo : {1, 3, 5, 10, 20}) {
    const uint8_t rows = combo_tier(combo).flameRows;
    FlameField<FLAME_LEFT_COLUMNS> field(FLAME_LEFT_SEED);
    FlameDriver steady;
    int reached = -1;
    for (int i = 0; i < 16; ++i) {
      driver.step(rows);
      assert(driver.height() >= previousHeight);
      previousHeight = driver.height();
      if (reached < 0 && driver.rows() * 10 >= rows * 9) reached = i + 1;
    }
    assert(reached > 0 && reached * FLAME_STEP_MS <= 700); // 9割に届くまで約0.5秒。
    int total = 0;
    for (int i = 0; i < 240; ++i) {
      steady.step(rows);
      field.step(steady.fuel());
      if (i >= 120) total += flame_height(field, steady.scale());
    }
    const int average = total / 120;
    assert(average > previousFlame && average <= FLAME_ROWS);
    previousFlame = average;
  }
  assert(previousFlame >= FLAME_ROWS * 3 / 4); // 20以上はほぼ全高。

  // 色は、炎・明るい炎・火の粉・煙のどれも上限を超えない。
  for (uint8_t level = 0; level < FLAME_LEVELS; ++level) {
    assert(within_flame_limit(flame_color(level, false)) && within_flame_limit(flame_color(level, true)));
    if (level) assert(flame_color(level, true) != flame_color(level, false)); // 20以上は1段明るい。
  }
  assert(flame_color(0, false) == 0 && within_flame_limit(art_rgb(FLAME_SPARK_RGB)));
  for (uint8_t level = 0; level <= SMOKE_LEVELS; ++level)
    assert(within_flame_limit(smoke_color(0, level)) && within_flame_limit(smoke_color(1, level)));
  for (unsigned heat = 0; heat < 256; ++heat)
    for (unsigned scale = 0; scale < 256; scale += 5) assert(flame_level(uint8_t(heat), uint8_t(scale)) < FLAME_LEVELS);

  // 炎の矩形の外に描かない。左は格子（x=0〜119、x=0は描画のクリップで消す）、右は x=320〜359 で、y=240の線に届かない。
  FlameField<FLAME_LEFT_COLUMNS> left(FLAME_LEFT_SEED);
  FlameField<FLAME_RIGHT_COLUMNS> right(FLAME_RIGHT_SEED);
  for (int i = 0; i < 200; ++i) {
    left.step(255);
    right.step(255);
    for_each_flame_run(left, 4, FLAME_LEFT_X, [](int x, int y, int w, int h, uint8_t level) {
      assert(level > 0 && x >= 0 && x + w <= LEFT_COLUMN_X + LEFT_COLUMN_WIDTH && y >= 0 && y + h <= 240);
    });
    for_each_flame_run(right, 4, FLAME_RIGHT_X, [](int x, int y, int w, int h, uint8_t) {
      assert(x >= RIGHT_MARGIN_X && x + w <= RIGHT_MARGIN_X + RIGHT_MARGIN_WIDTH && y >= 0 && y + h <= 240);
      assert(x + w <= 360);
    });
  }
  for (int columns : {FLAME_LEFT_COLUMNS, FLAME_RIGHT_COLUMNS}) {
    for (uint32_t age = 0; age < 300; ++age)
      for (uint32_t index = 0; index <= age; ++index) {
        FlameSpark spark;
        if (!flame_spark(7, columns, index, age, 0, spark)) continue;
        assert(spark.column >= 0 && spark.column < columns && spark.row >= 0 && spark.row < FLAME_ROWS);
        assert(age - index <= FLAME_SPARK_LIFE);
      }
    for (uint8_t rows : {uint8_t(0), uint8_t(10), uint8_t(58), uint8_t(60)})
      for (uint16_t ms = 0; ms < SMOKE_MS; ms += 7)
        for (uint8_t i = 0; i < flame_smoke_puffs(columns); ++i) {
          int x, y;
          flame_smoke_cell(FLAME_RIGHT_SEED, columns, i, rows, ms, x, y);
          assert(x >= 0 && x + FLAME_CELL <= columns * FLAME_CELL && y >= 0 && y + FLAME_CELL <= 240);
        }
  }
  // 火の粉は段階の炎より少し上で消える。
  FlameSpark spark;
  for (uint32_t index = 0; index < 100; ++index)
    if (flame_spark(7, FLAME_LEFT_COLUMNS, index, 100, 30, spark)) assert(spark.row >= 30);

  // コンボが切れたら火種を止め、煙の間は火種を止めたままにし、その後は新しい段の高さへ戻す。
  PlayingEffects effects;
  uint32_t now = 1000;
  effects.update(12, 0, true, now);
  for (; now < 4000; now += 16) effects.update(12, 0, true, now);
  assert(effects.frame().flameLit && effects.frame().flameFuel > 0 && effects.frame().sparks);
  effects.update(1, 0, true, now);
  assert(effects.frame().smokeMs == 0 && effects.frame().smokeRows >= combo_tier(12).flameRows * 9 / 10);
  assert(!effects.frame().sparks);
  uint32_t lastStep = effects.frame().flameStep;
  for (uint32_t end = now + SMOKE_MS - FLAME_STEP_MS; now < end; now += 16) {
    effects.update(1, 0, true, now);
    assert(effects.frame().flameFuel == 0 && effects.frame().flameLit && effects.frame().smokeMs != EFFECT_OFF);
    assert(effects.frame().flameStep >= lastStep);
    lastStep = effects.frame().flameStep;
  }
  for (uint32_t end = now + 2000; now < end; now += 16) effects.update(1, 0, true, now);
  assert(effects.frame().smokeMs == EFFECT_OFF && effects.frame().flameFuel > 0);
  // 減った後も10以上なら、火種を止めている間は火の粉を出さず、火種が戻ってから出し直す。煙はいまの炎の高さから上げる。
  PlayingEffects partial;
  uint32_t at = 0;
  partial.update(25, 0, true, at);
  partial.update(25, 0, true, at += 160); // 炎が育ちきる前に切れる。
  const uint8_t grown = partial.frame().flameFuel / FLAME_HEAT_PER_ROW;
  partial.update(12, 0, true, at += 16);
  assert(partial.frame().smokeMs == 0 && partial.frame().smokeRows <= grown + 1 && grown < 58);
  for (uint32_t end = at + SMOKE_MS - FLAME_STEP_MS; at < end; at += 16) {
    partial.update(12, 0, true, at);
    assert(!partial.frame().sparks && partial.frame().flameFuel == 0);
  }
  for (uint32_t end = at + 500; at < end; at += 16) {
    partial.update(12, 0, true, at);
    assert(!partial.frame().sparks || partial.frame().flameFuel > 0); // 火種より先に火の粉を出さない。
  }
  assert(partial.frame().sparks && partial.frame().flameFuel > 0 && partial.frame().sparkAge < 10);
  // 0に戻れば、残りの熱が消える段数の後は炎の段も止まる。
  effects.update(0, 0, true, now);
  for (uint32_t end = now + (FLAME_ROWS + 2) * FLAME_STEP_MS; now < end; now += 16) effects.update(0, 0, true, now);
  const EffectsFrame cold = effects.frame();
  assert(!cold.flameLit && cold.smokeMs == EFFECT_OFF);
  for (uint32_t end = now + 2000; now < end; now += 16) {
    effects.update(0, 0, true, now);
    assert(effects.frame() == cold);
  }
  // 20以上では明るいパレットを使う。
  effects.update(20, 0, true, now);
  for (uint32_t end = now + 500; now < end; now += 16) effects.update(20, 0, true, now);
  assert(effects.frame().flameHot && effects.frame().flameRows == 58);

  std::puts("PASS: flame field is reproducible from its seed, stays cold at combo 0, grows monotonically by tier, "
            "stays under the brightness cap and inside its rectangles, and dies out with smoke on a break");
}
