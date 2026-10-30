#ifndef PLAYING_EFFECTS_HPP_
#define PLAYING_EFFECTS_HPP_

#include <algorithm>
#include <cmath>
#include <cstdint>
#include "flame_field.hpp"
#include "pixel_art.hpp"

// ゲーム中の演出（薄い炎、流れるマスコット、順位の冠、コンボの区切り・BREAK・RANK UP）。
// すべて表示だけの演出で、値はstatusのcombo・rankをそのまま使い、筐体では数えない。LCD・FreeRTOSには依存しない。

// コンボの段階。炎の高さ（マス）と流れるマスコットの姿はこの表だけで決める。
enum class MascotForm : uint8_t { None, Slow, Sway, Pair, Blaze };
struct ComboTier {
  uint16_t minCombo;
  uint8_t flameRows; // 炎の高さ（60マス中）
  bool sparks;       // 炎の火の粉
  bool hot;          // 炎のパレットを1段明るくする
  MascotForm form;
};
constexpr ComboTier COMBO_TIERS[] = {
  {0, 0, false, false, MascotForm::None},   // 0: 炎なし
  {1, 10, false, false, MascotForm::None},  // 1〜2: 約1/6
  {3, 20, false, false, MascotForm::Slow},  // 3〜4: 約1/3。1体がゆっくり昇る
  {5, 30, false, false, MascotForm::Sway},  // 5〜9: 約1/2。頬を光らせ、揺れながら昇る
  {10, 45, true, false, MascotForm::Pair},  // 10〜19: 約3/4と火の粉。2体が交互に昇る
  {20, 58, true, true, MascotForm::Blaze},  // 20以上: ほぼ全高で明るく。炎をまとい、高速の1体が飛び抜ける
};
inline const ComboTier& combo_tier(uint16_t combo) {
  size_t i = sizeof(COMBO_TIERS) / sizeof(COMBO_TIERS[0]) - 1;
  while (combo < COMBO_TIERS[i].minCombo) --i;
  return COMBO_TIERS[i];
}

// コンボ数「x12」を出す下限。
constexpr uint16_t COMBO_SHOWN_MIN = 3;

// oldからnewへ増えたときに達した区切り（5、10、20、30、以降10ごと）のうち最大のもの。なければ0。
inline uint16_t milestone_crossed(uint16_t oldCombo, uint16_t newCombo) {
  if (newCombo <= oldCombo) return 0;
  const uint16_t milestone = newCombo >= 10 ? newCombo / 10 * 10 : newCombo >= 5 ? 5 : 0;
  return milestone > oldCombo ? milestone : 0;
}

inline Crown crown_for_rank(uint8_t rank) {
  return rank == 1 ? Crown::Gold : rank == 2 ? Crown::Silver : rank == 3 ? Crown::Bronze : Crown::None;
}

// 演出の長さ（ms）。進行度は炎の1段（FLAME_STEP_MS）ごとに進める。
constexpr uint16_t MILESTONE_MS = 600;
constexpr uint16_t RANK_UP_MS = 800;
constexpr uint16_t BREAK_TEXT_MS = 500;
constexpr uint16_t BREAK_FALL_MS = 500;               // しょんぼりの顔で落ちる
constexpr uint16_t BREAK_MS = BREAK_FALL_MS + SMOKE_MS; // 落ちてから煙になって消えるまで
constexpr uint16_t RANK_BLINK_MS = 120;               // 情報欄の順位の数字を緑に点滅させる周期
constexpr uint16_t CELEBRATE_FLIP_MS = 132;           // 喜びの顔で左右反転を挟み、一回転したように見せる
constexpr uint16_t EFFECT_OFF = 0xffff;

// 描画側へ渡す演出の状態。炎の熱の格子は入れず、描画側がflameStepまで再現する。
struct EffectsFrame {
  uint8_t epoch = 0;          // 演出をやり直すたびに変わる。描画側の炎の格子を消す
  // 炎。flameStepは炎が見えている間だけ1段ずつ進む
  uint32_t flameStep = 0;
  uint8_t flameFuel = 0;      // 火種の熱（1/4マス単位）。0は火種なし
  uint8_t flameScale = 0;     // パレットの正規化
  uint8_t flameRows = 0;      // 段階の炎の高さ。火の粉はこれより少し上で消える
  bool flameLit = false;      // 熱が残っている可能性がある
  bool flameHot = false;
  uint32_t sparkAge = 0;      // 火の粉が出始めてからの段数
  bool sparks = false;
  uint16_t smokeMs = EFFECT_OFF; // コンボが切れたときの炎の煙
  uint8_t smokeRows = 0;      // 煙を上げる炎の高さ
  // 流れるマスコット
  MascotForm form = MascotForm::None;
  uint32_t flightMs = 0;      // 下端から出直してからの時間
  uint8_t blink = 0;          // まばたき中の体（bit）
  // 区切り・順位・BREAK
  uint16_t milestoneMs = EFFECT_OFF;
  uint16_t milestone = 0;
  uint16_t rankUpMs = EFFECT_OFF;
  uint8_t rankUpRank = 0;
  uint16_t breakTextMs = EFFECT_OFF;
  uint16_t breakMs = EFFECT_OFF; // しょんぼりの顔で落ちて煙になる
  MascotForm brokenForm = MascotForm::None;
  uint32_t brokenFlightMs = 0;
};

inline bool same_flame(const EffectsFrame& a, const EffectsFrame& b) {
  return a.epoch == b.epoch && a.flameStep == b.flameStep && a.flameFuel == b.flameFuel &&
         a.flameScale == b.flameScale && a.flameRows == b.flameRows && a.flameLit == b.flameLit &&
         a.flameHot == b.flameHot && a.sparkAge == b.sparkAge && a.sparks == b.sparks &&
         a.smokeMs == b.smokeMs && a.smokeRows == b.smokeRows;
}
inline bool operator==(const EffectsFrame& a, const EffectsFrame& b) {
  return same_flame(a, b) && a.form == b.form && a.flightMs == b.flightMs && a.blink == b.blink &&
         a.milestoneMs == b.milestoneMs && a.milestone == b.milestone && a.rankUpMs == b.rankUpMs &&
         a.rankUpRank == b.rankUpRank && a.breakTextMs == b.breakTextMs && a.breakMs == b.breakMs &&
         a.brokenForm == b.brokenForm && a.brokenFlightMs == b.brokenFlightMs;
}
inline bool operator!=(const EffectsFrame& a, const EffectsFrame& b) { return !(a == b); }
// 比較は項目を並べて書いている。項目を足したら、same_flame・operator==にも足してからこの値を直す。
static_assert(sizeof(EffectsFrame) == 52, "add new EffectsFrame fields to same_flame / operator==");

// 炎の高さを段階の高さへ約0.5秒で追従させる。コンボが切れたら火種を止め、煙の間は止めたままにする。
class FlameDriver {
public:
  static constexpr int FOLLOW = 64; // 1段ごとに差の1/4ずつ近づける（8段・約0.5秒で約9割）。
  static constexpr uint8_t HOLD_STEPS = (SMOKE_MS + FLAME_STEP_MS - 1) / FLAME_STEP_MS;

  void step(uint8_t targetRows) {
    if (hold_) {
      --hold_;
    } else {
      const int diff = targetRows * 16 - height_;
      int move = diff * FOLLOW / 256;
      if (!move && diff) move = diff > 0 ? 1 : -1;
      height_ += move;
    }
    scale_ = std::max<unsigned>(fuel(), scale_ * 238 / 256);
    sinceFuel_ = fuel() ? 0 : std::min<uint8_t>(sinceFuel_ + 1, FLAME_ROWS);
  }
  void cut() {
    height_ = 0;
    hold_ = HOLD_STEPS;
  }
  // 1/16マス単位の高さ。
  int height() const { return height_; }
  uint8_t rows() const { return static_cast<uint8_t>(height_ / 16); }
  // 火種の熱（1/4マス）。半マス未満なら火種を止める。
  uint8_t fuel() const { return height_ >= 8 ? static_cast<uint8_t>(height_ / 4) : 0; }
  uint8_t scale() const { return scale_; }
  // 火種を止めてもFLAME_ROWS段までは熱が残り得る。それを過ぎれば格子はすべて0になる。
  bool lit() const { return sinceFuel_ < FLAME_ROWS; }

private:
  int height_ = 0;
  uint8_t scale_ = 0;
  uint8_t hold_ = 0;
  uint8_t sinceFuel_ = FLAME_ROWS;
};

// まばたきの時刻を決める。間隔は2〜4秒で、シードを指定すれば同じ並びになる。
class BlinkTimer {
public:
  static constexpr uint32_t MIN_INTERVAL_MS = 2000, MAX_INTERVAL_MS = 4000;
  explicit BlinkTimer(uint32_t seed = 1) : rng_(seed) { next_ = interval(); }
  // msは演出を始めてからの時間（単調に増える）。
  bool update(uint32_t ms) {
    while (ms >= next_ + MASCOT_BLINK_MS) next_ += interval();
    return ms >= next_;
  }
  uint32_t next_blink() const { return next_; }
private:
  uint32_t interval() { return MIN_INTERVAL_MS + rng_.below(MAX_INTERVAL_MS - MIN_INTERVAL_MS + 1); }
  Xorshift32 rng_;
  uint32_t next_ = 0;
};

// ---- 流れるマスコットの位置（時刻だけで決まる） ----
constexpr int MASCOT_SCALE = 2;
constexpr int MASCOT_DRAWN_WIDTH = MASCOT_WIDTH * MASCOT_SCALE, MASCOT_DRAWN_HEIGHT = MASCOT_HEIGHT * MASCOT_SCALE;
constexpr int MASCOT_DRAWN_MARGIN = MASCOT_AURA_MARGIN * MASCOT_SCALE;
// 縁取りを含めて下端の外（y=240以上）から出て、上端の外へ消える。
constexpr int FLIGHT_BOTTOM = LEFT_COLUMN_Y + LEFT_COLUMN_HEIGHT + MASCOT_DRAWN_MARGIN + MASCOT_SCALE;
constexpr int FLIGHT_TOP = LEFT_COLUMN_Y - MASCOT_DRAWN_HEIGHT - MASCOT_DRAWN_MARGIN - MASCOT_SCALE;
constexpr uint8_t MASCOT_SLOTS = 3;
constexpr uint32_t SLOW_PERIOD_MS = 3000, SWAY_PERIOD_MS = 2500, PAIR_PERIOD_MS = 2500;
constexpr uint32_t DASH_INTERVAL_MS = 4000, DASH_MS = 1500;

struct MascotFlight {
  int16_t x, y;      // 枠の左上
  uint16_t flapMs;   // 羽ばたき1コマの長さ
  bool dash;         // 高速で飛び抜ける1体（喜びの顔）
};

inline int flight_y(uint32_t phaseMs, uint32_t periodMs) {
  return FLIGHT_BOTTOM - int(int64_t(FLIGHT_BOTTOM - FLIGHT_TOP) * phaseMs / periodMs);
}
inline int sway(uint32_t phaseMs, uint32_t periodMs, float offset, int amplitude) {
  constexpr float TAU = 6.28318530718f;
  return int(std::lround(std::sin(TAU * 2 * float(phaseMs) / float(periodMs) + offset) * float(amplitude)));
}

// 姿formのslot番目の体の、下端から出直してからms後の位置。見えなければfalse。
inline bool mascot_flight(MascotForm form, uint8_t slot, uint32_t ms, MascotFlight& flight) {
  switch (form) {
    case MascotForm::None: return false;
    case MascotForm::Slow:
      if (slot) return false;
      flight = {36, int16_t(flight_y(ms % SLOW_PERIOD_MS, SLOW_PERIOD_MS)), 160, false};
      return true;
    case MascotForm::Sway: {
      if (slot) return false;
      const uint32_t phase = ms % SWAY_PERIOD_MS;
      flight = {int16_t(36 + sway(phase, SWAY_PERIOD_MS, 0, 14)), int16_t(flight_y(phase, SWAY_PERIOD_MS)), 130, false};
      return true;
    }
    case MascotForm::Pair:
    case MascotForm::Blaze: {
      if (slot == 2) {
        if (form != MascotForm::Blaze) return false;
        const uint32_t phase = ms % DASH_INTERVAL_MS;
        if (phase >= DASH_MS) return false;
        flight = {36, int16_t(flight_y(phase, DASH_MS)), 60, true};
        return true;
      }
      // 2体目は半周期遅れて下端から出る。
      const uint32_t delay = slot * PAIR_PERIOD_MS / 2;
      if (ms < delay) return false;
      const uint32_t phase = (ms - delay) % PAIR_PERIOD_MS;
      flight = {int16_t((slot ? 58 : 14) + sway(phase, PAIR_PERIOD_MS, float(slot), 8)),
                int16_t(flight_y(phase, PAIR_PERIOD_MS)), 120, false};
      return true;
    }
  }
  return false;
}

// ---- 左の列に描くもの ----
struct LeftMascot {
  MascotPose pose;
  int16_t x, y;
  bool flip;
};

inline uint16_t celebrate_ms(const EffectsFrame& f) {
  return std::min(f.milestoneMs, f.rankUpMs);
}

// 流れているマスコット（最大3体）。区切り・RANK UPの間は喜びの顔で速く羽ばたき、左右反転を挟む。
inline uint8_t flying_mascots(const EffectsFrame& f, uint8_t rank, LeftMascot (&out)[MASCOT_SLOTS]) {
  uint8_t count = 0;
  const uint16_t celebrate = celebrate_ms(f);
  for (uint8_t slot = 0; slot < MASCOT_SLOTS; ++slot) {
    MascotFlight flight;
    if (!mascot_flight(f.form, slot, f.flightMs, flight)) continue;
    const bool joy = flight.dash || celebrate != EFFECT_OFF;
    const uint16_t flapMs = celebrate != EFFECT_OFF ? 70 : flight.flapMs;
    LeftMascot& m = out[count++];
    m = {};
    m.pose.wing = static_cast<uint8_t>(f.flightMs / flapMs % 4);
    m.pose.legs = static_cast<uint8_t>(f.flightMs / MASCOT_LEGS_PART.frameMs % 2);
    m.pose.face = joy ? MascotFace::Joy : (f.blink & (1u << slot)) ? MascotFace::Blink : MascotFace::Normal;
    m.pose.crown = crown_for_rank(rank);
    m.pose.blush = f.form >= MascotForm::Sway && f.flightMs / 250 % 2 == 0;
    m.pose.aura = f.form == MascotForm::Blaze;
    m.pose.auraTick = static_cast<uint8_t>(f.flightMs / FLAME_STEP_MS);
    m.x = flight.x;
    m.y = static_cast<int16_t>(flight.y - (m.pose.wing == MASCOT_WING_DOWN_FRAME ? MASCOT_SCALE : 0));
    m.flip = celebrate != EFFECT_OFF && celebrate / CELEBRATE_FLIP_MS % 2 == 1;
  }
  return count;
}

// コンボが切れたときに流れていたマスコット。冠は付けたまま「× ×」の顔で羽ばたきを止め、少し落ちる。
// 落ち終えたら0を返し、代わりに煙（broken_smoke_origin）を出す。
constexpr int BREAK_FALL_PX = 28;
inline uint8_t broken_mascots(const EffectsFrame& f, uint8_t rank, LeftMascot (&out)[MASCOT_SLOTS]) {
  if (f.breakMs == EFFECT_OFF || f.breakMs >= BREAK_FALL_MS) return 0;
  uint8_t count = 0;
  const int fall = BREAK_FALL_PX * f.breakMs * f.breakMs / (BREAK_FALL_MS * BREAK_FALL_MS);
  for (uint8_t slot = 0; slot < MASCOT_SLOTS; ++slot) {
    MascotFlight flight;
    if (!mascot_flight(f.brokenForm, slot, f.brokenFlightMs, flight)) continue;
    LeftMascot& m = out[count++];
    m = {};
    m.pose.wing = 1;
    m.pose.face = MascotFace::Sad;
    m.pose.crown = crown_for_rank(rank);
    m.x = flight.x;
    m.y = static_cast<int16_t>(flight.y + fall);
    m.flip = false;
  }
  return count;
}

// 落ち終えたマスコットの煙の出どころ（体の中心）。煙の間でなければfalse。
inline bool broken_smoke(const EffectsFrame& f, uint8_t slot, int& x, int& y, uint16_t& ms) {
  if (f.breakMs == EFFECT_OFF || f.breakMs < BREAK_FALL_MS || f.breakMs >= BREAK_MS) return false;
  MascotFlight flight;
  if (!mascot_flight(f.brokenForm, slot, f.brokenFlightMs, flight)) return false;
  x = flight.x + MASCOT_DRAWN_WIDTH / 2;
  y = flight.y + BREAK_FALL_PX + MASCOT_DRAWN_HEIGHT / 2;
  ms = f.breakMs - BREAK_FALL_MS;
  return true;
}
constexpr uint8_t MASCOT_SMOKE_PUFFS = 7;
constexpr int MASCOT_SMOKE_SIZE = 8, MASCOT_SMOKE_SPREAD = 22, MASCOT_SMOKE_RISE = 10;

// 2体が昇る姿では、通り道に火の粉を散らす。少し前の位置に置き、だんだん暗くする。
constexpr uint8_t TRAIL_STEPS = 5;
constexpr int TRAIL_SIZE = 4;
constexpr uint16_t TRAIL_COLOR = art_rgb(0xff8a1e); // 炎の縁取りの橙
struct TrailSpark {
  int16_t x, y;
  uint8_t level; // 1〜TRAIL_STEPS
};
inline bool trail_spark(const EffectsFrame& f, uint32_t seed, uint8_t slot, uint8_t age, TrailSpark& spark) {
  if (f.form < MascotForm::Pair || slot > 1 || age == 0 || age > TRAIL_STEPS) return false;
  const uint32_t step = f.flightMs / FLAME_STEP_MS;
  if (step < age) return false;
  const uint32_t emitted = step - age;
  MascotFlight flight;
  if (!mascot_flight(f.form, slot, emitted * FLAME_STEP_MS, flight)) return false;
  const uint32_t h = effect_hash(seed, emitted, slot + 3);
  spark = {int16_t(flight.x + 8 + int(h % 32)), int16_t(flight.y + MASCOT_DRAWN_HEIGHT - 4 + int(h >> 8) % 6 + age * 2),
           uint8_t(TRAIL_STEPS + 1 - age)};
  return true;
}

// 区切り・RANK UPの火花。十数個の点が中心から外へ散る。
constexpr uint8_t CELEBRATE_SPARKS = 14;
constexpr int CELEBRATE_SPARK_SIZE = 4;
inline void celebrate_spark(uint8_t index, uint16_t ms, uint16_t durationMs, int cx, int cy, int rx, int ry,
                            int& x, int& y) {
  constexpr float TAU = 6.28318530718f;
  const float angle = TAU * index / CELEBRATE_SPARKS;
  const float k = float(std::min(ms, durationMs)) / float(durationMs);
  x = cx + int(std::lround(std::cos(angle) * (8 + k * rx))) - CELEBRATE_SPARK_SIZE / 2;
  y = cy + int(std::lround(std::sin(angle) * (8 + k * ry))) - CELEBRATE_SPARK_SIZE / 2;
}

// 左の列の文字と演出の位置。中心は列の中央。
constexpr int LEFT_CENTER_X = LEFT_COLUMN_X + LEFT_COLUMN_WIDTH / 2;
constexpr int RANK_UP_TEXT_Y = 60, RANK_UP_NUMBER_Y = 80;   // 数字3と2の間（y=52〜108）
constexpr int RANK_UP_SPARK_Y = 80, RANK_UP_SPARK_RX = 46, RANK_UP_SPARK_RY = 30;
constexpr int MILESTONE_TEXT_Y = 150;                       // 数字2と1の間。BREAKも同じ位置
constexpr int MILESTONE_SPARK_Y = 158, MILESTONE_SPARK_R = 46;
constexpr int COMBO_TEXT_X = 8, COMBO_TEXT_Y = 218;         // 数字1の下
constexpr int TEXT_BACK_PAD = 2;
constexpr uint16_t RANK_UP_GREEN = art_rgb(0x7dff7d);
constexpr uint16_t MILESTONE_YELLOW = art_rgb(0xffd23f);
constexpr uint16_t COMBO_YELLOW = art_rgb(0xffe14a);
constexpr uint16_t BREAK_GRAY = art_rgb(0xc6c6c6);

// UPの矢印（5×5を2倍）3本が下から上へ昇る。
constexpr uint8_t RANK_UP_ARROWS = 3;
inline void rank_up_arrow(uint8_t index, uint16_t ms, int& x, int& y) {
  static constexpr int16_t X[RANK_UP_ARROWS] = {12, 52, 92};
  static constexpr uint16_t DELAY[RANK_UP_ARROWS] = {0, 120, 60};
  const uint32_t moving = ms > DELAY[index] ? ms - DELAY[index] : 0;
  x = X[index];
  y = 150 - int(110 * std::min<uint32_t>(moving, RANK_UP_MS) / RANK_UP_MS);
}

// Font0（1文字6×8、右端の1列は空き）で、左の列に収まる最大の文字サイズ（2か1）。
inline int left_text_size(size_t characters) {
  return int(characters * 6 * 2) - 2 <= LEFT_COLUMN_WIDTH ? 2 : 1;
}

// 炎の煙のマス。炎の上端から少し広がりながら上がる。
constexpr int FLAME_SMOKE_SPREAD = 6, FLAME_SMOKE_RISE = 24;
inline uint8_t flame_smoke_puffs(int columns) { return static_cast<uint8_t>(std::max(2, columns / 4)); }
// index番目の煙のマスの位置（格子の左端からの画素）。炎の矩形の中に収める。
inline void flame_smoke_cell(uint32_t seed, int columns, uint8_t index, uint8_t rows, uint16_t ms, int& x, int& y) {
  const uint8_t count = flame_smoke_puffs(columns);
  const SmokePuff puff = smoke_puff(seed, index, count, ms, FLAME_SMOKE_SPREAD, FLAME_SMOKE_RISE);
  x = (index * columns / count + columns / count / 2) * FLAME_CELL + puff.dx;
  y = (FLAME_ROWS - std::max<int>(rows, 2)) * FLAME_CELL + puff.dy;
  x = std::min(std::max(x, 0), columns * FLAME_CELL - FLAME_CELL);
  y = std::min(std::max(y, 0), FLAME_ROWS * FLAME_CELL - FLAME_CELL);
}

// コンボと順位から演出の状態を作る。ゲーム側がスナップショットの発行ごとに呼ぶ。
class PlayingEffects {
public:
  static constexpr uint32_t SEED = 0x5600;
  static constexpr uint32_t NONE = UINT32_MAX;
  static constexpr uint32_t MAX_CATCH_UP_STEPS = 256;

  explicit PlayingEffects(uint32_t seed = SEED) : seed_(seed) {}

  // enabledは表示値が揃った通常のPlaying画面。そうでなければ演出の状態を消す。
  void update(uint16_t combo, uint8_t rank, bool enabled, uint32_t nowMs) {
    if (!enabled) {
      if (running_) {
        running_ = false;
        frame_ = {};
      }
      return;
    }
    if (!running_) start(combo, rank, nowMs);
    const uint32_t target = (nowMs - startMs_) / FLAME_STEP_MS;
    if (target - step_ > MAX_CATCH_UP_STEPS) step_ = target - MAX_CATCH_UP_STEPS;
    while (step_ < target) advance();
    nowMs_ = nowMs;
    if (combo != combo_) change_combo(combo);
    change_rank(rank, nowMs);
    compose();
  }

  const EffectsFrame& frame() const { return frame_; }
  bool running() const { return running_; }

  // 情報欄の順位の数字を緑で描く間。RANK UPと同じ期間、点滅させる。
  bool rank_flash() const {
    if (!running_ || rankUpStep_ == NONE) return false;
    const uint32_t elapsed = nowMs_ - rankUpAtMs_;
    return elapsed < RANK_UP_MS && elapsed / RANK_BLINK_MS % 2 == 0;
  }

private:
  void start(uint16_t combo, uint8_t rank, uint32_t nowMs) {
    running_ = true;
    ++epoch_;
    if (!epoch_) epoch_ = 1;
    startMs_ = nowMs;
    step_ = 0;
    combo_ = combo;
    rank_ = rank;
    flame_ = FlameDriver{};
    flameStep_ = 0;
    // 画面を離れている間の変化は演出しない。今の段階の姿を下端から出す。
    flightStartStep_ = 0;
    sparkStartStep_ = combo_tier(combo).sparks ? 0 : NONE;
    smokeStep_ = milestoneStep_ = rankUpStep_ = breakTextStep_ = breakStep_ = NONE;
    for (uint8_t i = 0; i < MASCOT_SLOTS; ++i) blinks_[i] = BlinkTimer(seed_ + 17 * (i + 1));
  }

  bool active(uint32_t since, uint16_t durationMs) const {
    return since != NONE && (step_ - since) * FLAME_STEP_MS < durationMs;
  }
  uint16_t progress(uint32_t since, uint16_t durationMs) const {
    return active(since, durationMs) ? static_cast<uint16_t>((step_ - since) * FLAME_STEP_MS) : EFFECT_OFF;
  }
  bool smoking() const { return active(smokeStep_, SMOKE_MS); }

  void advance() {
    ++step_;
    flame_.step(combo_tier(combo_).flameRows);
    if (flame_.lit() || smoking()) ++flameStep_;
  }

  void change_combo(uint16_t combo) {
    const uint16_t old = combo_;
    const ComboTier& before = combo_tier(old);
    const ComboTier& after = combo_tier(combo);
    combo_ = combo;
    if (combo > old) {
      // 段階が上がったら、その段階の姿を下端からすぐに出す。BREAKの最中なら終わってから出す。
      if (after.form > before.form) flightStartStep_ = std::max(step_, flightStartStep_);
      if (after.sparks && !before.sparks) sparkStartStep_ = step_;
      if (const uint16_t milestone = milestone_crossed(old, combo)) {
        milestoneStep_ = step_;
        milestone_ = milestone;
      }
      return;
    }
    // 減ったらコンボが切れた。炎は火種を止めて自然に消し、煙を上げる。
    // 煙は、育ちきる前に切れた場合も含めて、いまの炎の高さから上げる。
    smokeRows_ = flame_.rows();
    if (before.flameRows) smokeStep_ = step_;
    flame_.cut();
    // 火種を止めている間は火の粉も出さない。減った後も10以上なら、火種が戻ってから出し直す。
    // 火種はHOLD_STEPS段止めた次の段から戻る。
    sparkStartStep_ = after.sparks ? step_ + FlameDriver::HOLD_STEPS + 1 : NONE;
    milestoneStep_ = NONE;
    if (old < COMBO_SHOWN_MIN) return;
    breakTextStep_ = step_;
    if (before.form != MascotForm::None && step_ >= flightStartStep_) {
      breakStep_ = step_;
      brokenForm_ = before.form;
      brokenFlightMs_ = (step_ - flightStartStep_) * FLAME_STEP_MS;
    }
    // 新しい段階の姿は、落ちて煙になり終えてから下端から出す。
    flightStartStep_ = step_ + (BREAK_MS + FLAME_STEP_MS - 1) / FLAME_STEP_MS;
  }

  // 上がったとき（1〜8の値が前より小さくなったとき）だけ演出する。nullから順位が付いたとき、下がったときは出さない。
  void change_rank(uint8_t rank, uint32_t nowMs) {
    if (rank && rank_ && rank < rank_) {
      rankUpStep_ = step_;
      rankUpAtMs_ = nowMs;
      rankUpRank_ = rank;
    }
    if (!rank) rankUpStep_ = NONE;
    rank_ = rank;
  }

  void compose() {
    EffectsFrame f;
    f.epoch = epoch_;
    // 炎が見えない間はflameStepが進まないので、そのまま入れても差分にならない。
    f.flameStep = flameStep_;
    const ComboTier& tier = combo_tier(combo_);
    if (flame_.lit() || smoking()) {
      f.flameLit = flame_.lit();
      if (f.flameLit) {
        f.flameFuel = flame_.fuel();
        f.flameScale = flame_.scale();
        f.flameRows = tier.flameRows;
        f.flameHot = tier.hot;
      }
    }
    if (smoking()) {
      f.smokeMs = progress(smokeStep_, SMOKE_MS);
      f.smokeRows = smokeRows_;
    }
    if (sparkStartStep_ != NONE && tier.sparks && step_ >= sparkStartStep_) {
      f.sparks = true;
      f.sparkAge = step_ - sparkStartStep_;
    }
    if (tier.form != MascotForm::None && step_ >= flightStartStep_) {
      f.form = tier.form;
      f.flightMs = (step_ - flightStartStep_) * FLAME_STEP_MS;
      for (uint8_t i = 0; i < MASCOT_SLOTS; ++i)
        if (blinks_[i].update(step_ * FLAME_STEP_MS)) f.blink |= 1u << i;
    }
    f.milestoneMs = progress(milestoneStep_, MILESTONE_MS);
    if (f.milestoneMs != EFFECT_OFF) f.milestone = milestone_;
    f.rankUpMs = progress(rankUpStep_, RANK_UP_MS);
    if (f.rankUpMs != EFFECT_OFF) f.rankUpRank = rankUpRank_;
    f.breakTextMs = progress(breakTextStep_, BREAK_TEXT_MS);
    f.breakMs = progress(breakStep_, BREAK_MS);
    if (f.breakMs != EFFECT_OFF) {
      f.brokenForm = brokenForm_;
      f.brokenFlightMs = brokenFlightMs_;
    }
    frame_ = f;
  }

  uint32_t seed_;
  bool running_ = false;
  uint8_t epoch_ = 0;
  uint32_t startMs_ = 0, nowMs_ = 0;
  uint32_t step_ = 0;
  uint16_t combo_ = 0;
  uint8_t rank_ = 0;
  FlameDriver flame_;
  uint32_t flameStep_ = 0;
  uint32_t flightStartStep_ = 0;
  uint32_t sparkStartStep_ = NONE;
  uint32_t smokeStep_ = NONE;
  uint8_t smokeRows_ = 0;
  uint32_t milestoneStep_ = NONE;
  uint16_t milestone_ = 0;
  uint32_t rankUpStep_ = NONE;
  uint32_t rankUpAtMs_ = 0;
  uint8_t rankUpRank_ = 0;
  uint32_t breakTextStep_ = NONE;
  uint32_t breakStep_ = NONE;
  MascotForm brokenForm_ = MascotForm::None;
  uint32_t brokenFlightMs_ = 0;
  BlinkTimer blinks_[MASCOT_SLOTS];
  EffectsFrame frame_{};
};

#endif // PLAYING_EFFECTS_HPP_
