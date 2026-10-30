#ifndef TOP_PERCENT_ART_HPP_
#define TOP_PERCENT_ART_HPP_

#include <algorithm>
#include <cmath>
#include <cstdint>
#include "effect_random.hpp"
#include "gain_animation.hpp"
#include "pixel_art.hpp"
#include "player_status.hpp"

// 結果②（上位%）のヘッダーに出すマスコットのドット絵。上位%が小さいほど派手にする。
// 表示だけの演出で、値はsummaryのtopPercent・bottomHalfをそのまま使う。LCD・FreeRTOSには依存しない。

// ヘッダーの空き（RESULTとモード表示の右、自ノードの札の左）。小物・花火・紙吹雪もこの中に描く。
constexpr int TOP_ART_X = 200, TOP_ART_Y = 4, TOP_ART_WIDTH = 156, TOP_ART_HEIGHT = 73;
constexpr int TOP_ART_SCALE = 3;
constexpr uint16_t TOP_ART_CYCLE_MS = 2000; // 絵の1周
constexpr uint16_t TOP_ART_STEP_MS = 50;    // 絵を進める間隔（20fps）

enum class TopProp : uint8_t { None, Crown, Trophy, Star, Medal };
enum class TopMotion : uint8_t { Hop, SpinHop, Swing, Flap, Droop };
enum class TopBurst : uint8_t { None, Fireworks, StreakStars, Confetti };

struct TopArtTier {
  uint8_t maxPercent; // 上位%がこの値以下（1〜50）。下位50%の行では使わない
  TopProp prop;
  TopMotion motion;
  MascotFace face;
  uint16_t flapMs;    // 羽ばたき1コマの長さ
  TopBurst burst;     // 派手な動き。初回の通常遷移で開いた②で1周だけ出す
};

// 段階の表。区切りは仮の値で、この表だけを直せば変えられる。
// 26〜50の行は汎用の獲得演出（喜びの顔で羽ばたくマスコットと紙吹雪）で、どの行もこれに差し替えられる。
constexpr TopArtTier TOP_ART_TIERS[] = {
  {1, TopProp::Crown, TopMotion::Hop, MascotFace::Joy, 100, TopBurst::Fireworks},      // 王冠で跳ね、後ろで花火が3発
  {5, TopProp::Trophy, TopMotion::Flap, MascotFace::Joy, 150, TopBurst::StreakStars},  // トロフィーを抱え、光の筋と星
  {10, TopProp::Star, TopMotion::SpinHop, MascotFace::Joy, 100, TopBurst::Confetti},   // 大きな星を持ち、回りながら跳ねる
  {25, TopProp::Medal, TopMotion::Swing, MascotFace::Joy, 150, TopBurst::Confetti},    // メダルを下げて揺れ、メダルがきらめく
  {50, TopProp::None, TopMotion::Flap, MascotFace::Joy, 100, TopBurst::Confetti},      // パタパタ羽ばたく
};
// 下位50%。値を伏せるので段階はなく、全員に同じ絵を出す。祝う動きはない。
constexpr TopArtTier TOP_ART_BOTTOM{0, TopProp::None, TopMotion::Droop, MascotFace::Sad, 0, TopBurst::None};

// ②に出す絵の段階。②を出さない結果（対象外・手動の終了・記録を使えない起動）ではnullptr。
inline const TopArtTier* top_art_tier(const FinalSummary& summary) {
  if (summary.bottomHalf) return &TOP_ART_BOTTOM;
  if (!summary.topPercent) return nullptr;
  for (const auto& tier : TOP_ART_TIERS)
    if (summary.topPercent <= tier.maxPercent) return &tier;
  return nullptr;
}

// 描画側へ渡す絵の状態。
struct TopArtFrame {
  bool shown = false;
  bool burst = false; // 派手な動き（花火・光の筋と星・紙吹雪）を重ねる
  uint16_t ms = 0;    // 1周の中の時間。TOP_ART_STEP_MSごとに進む
};

// ---- マスコットと小物の位置（1周の中の時間だけで決まる） ----
// 小物はマスコットの枠（24×17）の中の位置（絵の画素）に置く。冠はマスコットの部品で載せる。
struct TopPropPlacement {
  const ArtPart* part;
  int8_t x, y;
};
inline TopPropPlacement top_prop(TopProp prop) {
  switch (prop) {
    case TopProp::Trophy: return {&TOP_TROPHY_PART, 7, 11}; // 体の前で抱える
    case TopProp::Star: return {&TOP_BIG_STAR_PART, 20, 3}; // 右の羽の先で持つ
    case TopProp::Medal: return {&TOP_MEDAL_PART, 8, 11};   // 口の下に下げる
    default: return {nullptr, 0, 0};
  }
}
constexpr int TOP_GLINT_X = 3, TOP_GLINT_Y = 2; // メダルの中のきらめきの位置

// マスコットの枠を横の中央に置き、小物を含めた下端を範囲の下端に合わせる。動きは上と左右にだけ動かす。
constexpr int TOP_ART_FIGURE_X = TOP_ART_X + (TOP_ART_WIDTH - MASCOT_WIDTH * TOP_ART_SCALE) / 2;
inline int top_art_rest_y(const TopArtTier& tier) {
  const TopPropPlacement prop = top_prop(tier.prop);
  const int rows = prop.part ? std::max(MASCOT_HEIGHT, prop.y + prop.part->height) : MASCOT_HEIGHT;
  return TOP_ART_Y + TOP_ART_HEIGHT - rows * TOP_ART_SCALE;
}

constexpr uint16_t TOP_HOP_MS = 1000, TOP_HOP_AIR_MS = 600; // 1秒に1回跳ね、0.6秒で着地する
constexpr int TOP_HOP_PX = 18;
constexpr uint16_t TOP_SPIN_FLIP_MS = 150;  // 跳んでいる間、左右反転を挟んで回って見せる
constexpr uint16_t TOP_BOB_MS = 1000;       // 羽ばたきながらゆっくり上下する
constexpr int TOP_BOB_PX = 3;
constexpr int TOP_SWING_PX = 6;             // メダルを下げて左右に揺れる幅（1周で1往復）
constexpr uint16_t TOP_DROOP_MS = 600;      // しょんぼり: 羽を上げている時間。その後、羽を下ろして体を沈める
constexpr uint16_t TOP_GLINT_FROM_MS = 1200, TOP_GLINT_PEAK_MS = 1300, TOP_GLINT_FADE_MS = 1450,
                   TOP_GLINT_TO_MS = 1550;

inline int top_art_hop(uint16_t ms) {
  const uint32_t phase = ms % TOP_HOP_MS;
  if (phase >= TOP_HOP_AIR_MS) return 0;
  return -int(4 * TOP_HOP_PX * phase * (TOP_HOP_AIR_MS - phase) / (uint32_t(TOP_HOP_AIR_MS) * TOP_HOP_AIR_MS));
}
// 円周の角度。ArduinoのPI・TWO_PIはマクロなので、その名前は使わない。
constexpr float TOP_ART_TAU = 6.28318530718f;
inline int top_art_wave(uint16_t ms, uint16_t periodMs, int amplitude) {
  return int(std::lround(std::sin(TOP_ART_TAU * float(ms % periodMs) / float(periodMs)) * float(amplitude)));
}
// 派手な動きが外へ広がるときのease-out（0〜1）。
inline float top_art_ease_out(float u) { return 1 - (1 - u) * (1 - u); }

// メダルのきらめきのコマ（小0・大1）。光らない間は-1。
inline int top_glint_frame(uint16_t ms) {
  if (ms < TOP_GLINT_FROM_MS || ms >= TOP_GLINT_TO_MS) return -1;
  return ms >= TOP_GLINT_PEAK_MS && ms < TOP_GLINT_FADE_MS ? 1 : 0;
}

struct TopArtFigure {
  MascotPose pose;
  int16_t x, y; // マスコットの枠の左上
  bool flip;
};

inline TopArtFigure top_art_figure(const TopArtTier& tier, uint16_t ms) {
  TopArtFigure f{};
  f.pose.face = tier.face;
  f.pose.crown = tier.prop == TopProp::Crown ? Crown::Gold : Crown::None;
  int dx = 0, dy = 0;
  if (tier.motion == TopMotion::Droop) {
    // 肩を落とす: 羽を上げた姿から、羽を下ろして体を1画素沈める。羽ばたかず、足も揺らさない。
    const bool up = ms < TOP_DROOP_MS;
    f.pose.wing = up ? 1 : MASCOT_WING_DOWN_FRAME;
    f.pose.legs = 1;
    dy = up ? -TOP_ART_SCALE : 0;
  } else {
    f.pose.wing = static_cast<uint8_t>(ms / tier.flapMs % 4);
    f.pose.legs = static_cast<uint8_t>(ms / MASCOT_LEGS_PART.frameMs % 2);
    switch (tier.motion) {
      case TopMotion::Hop:
      case TopMotion::SpinHop: {
        dy = top_art_hop(ms);
        const uint32_t phase = ms % TOP_HOP_MS;
        f.flip = tier.motion == TopMotion::SpinHop && phase < TOP_HOP_AIR_MS && phase / TOP_SPIN_FLIP_MS % 2 == 1;
        break;
      }
      case TopMotion::Swing: dx = top_art_wave(ms, TOP_ART_CYCLE_MS, TOP_SWING_PX); break;
      default: dy = -TOP_BOB_PX - top_art_wave(ms, TOP_BOB_MS, TOP_BOB_PX); break;
    }
    // 羽を下ろすコマでは体を1画素上げ、弾むように見せる（ゲーム中と同じ）。
    if (f.pose.wing == MASCOT_WING_DOWN_FRAME) dy -= TOP_ART_SCALE;
  }
  f.x = static_cast<int16_t>(TOP_ART_FIGURE_X + dx);
  f.y = static_cast<int16_t>(top_art_rest_y(tier) + dy);
  return f;
}

inline bool same_pose(const MascotPose& a, const MascotPose& b) {
  return a.wing == b.wing && a.face == b.face && a.legs == b.legs && a.crown == b.crown && a.blush == b.blush &&
         a.aura == b.aura && a.auraTick == b.auraTick;
}

// 同じ画素になるか。派手な動きの間は段ごとに変わるとみなし、静かな間はマスコットの姿と位置・きらめきだけを比べる。
inline bool same_top_art(const TopArtTier* tierA, const TopArtFrame& a, const TopArtTier* tierB, const TopArtFrame& b) {
  if (a.shown != b.shown) return false;
  if (!a.shown) return true;
  if (tierA != tierB || a.burst != b.burst) return false;
  if (a.burst) return a.ms == b.ms;
  const TopArtFigure fa = top_art_figure(*tierA, a.ms), fb = top_art_figure(*tierB, b.ms);
  return fa.x == fb.x && fa.y == fb.y && fa.flip == fb.flip && same_pose(fa.pose, fb.pose) &&
         (tierA->prop != TopProp::Medal || top_glint_frame(a.ms) == top_glint_frame(b.ms));
}

// ---- 派手な動き（1周の中の時間だけで決まる） ----
struct TopFirework {
  int16_t x, y;      // 開く位置
  uint16_t launchMs; // 打ち上げる時刻
  uint8_t radius;
  uint16_t color;
};
// マスコットの左・右・頭の上の後ろで開く。頭の上の1発は跳ねたマスコットに一部が隠れる。
constexpr TopFirework TOP_FIREWORKS[] = {
  {222, 26, 0, 16, art_rgb(0xffd23f)},
  {334, 26, 450, 16, art_rgb(0xff6fb0)},
  {278, 20, 900, 13, art_rgb(0x6fe0ff)},
};
constexpr uint16_t FIREWORK_RISE_MS = 250, FIREWORK_BURST_MS = 650;
constexpr uint8_t FIREWORK_SPARKS = 12, FIREWORK_INNER_SPARKS = 6; // 外側の輪と、半分ほどの半径の内側の輪
constexpr int FIREWORK_FALL_PX = 6;

struct TopStreak {
  int16_t y;
  uint16_t startMs;
};
// 光の筋は、マスコットの後ろを左から右へ横切る。
constexpr TopStreak TOP_STREAKS[] = {{16, 0}, {60, 250}};
constexpr uint16_t STREAK_MS = 600;
constexpr int STREAK_HEAD = 8, STREAK_SEGMENTS = 5, STREAK_SEGMENT = 8;
constexpr uint16_t STREAK_COLOR = art_rgb(0xffe14a);

// 星は、トロフィーから上半分の向きへ散り、瞬いてから消える。
constexpr uint8_t TOP_SPARKLES = 8;
constexpr int TOP_SPARKLE_SCALE = 2, TOP_SPARKLE_RX = 66, TOP_SPARKLE_RY = 46;
constexpr uint16_t SPARKLE_FROM_MS = 300, SPARKLE_SPREAD_MS = 700, SPARKLE_TO_MS = 1700;

constexpr uint8_t TOP_CONFETTI = 18;
constexpr int CONFETTI_SIZE = 3;
constexpr uint16_t CONFETTI_COLORS[] = {
  art_rgb(0xffd23f), art_rgb(0xf04848), art_rgb(0x50c850), art_rgb(0x40b8e8), 0xffff,
};
constexpr uint32_t TOP_ART_SEED = 0x5600;

// 描画を範囲の中へ切り取る描画先。範囲の外の画素は描かない。
template <typename Target>
struct TopArtClip {
  Target& target;
  void fillRect(int x, int y, int w, int h, uint16_t color) {
    const int left = std::max(x, TOP_ART_X), top = std::max(y, TOP_ART_Y);
    const int right = std::min(x + w, TOP_ART_X + TOP_ART_WIDTH), bottom = std::min(y + h, TOP_ART_Y + TOP_ART_HEIGHT);
    if (left < right && top < bottom) target.fillRect(left, top, right - left, bottom - top, color);
  }
};

// 小物をマスコットの枠の中の位置(x, y)に描く。マスコットが左右反転していれば、枠の中で左右対称の位置へ反転して描く。
template <typename Target>
void draw_top_prop(Target& target, const ArtPart& part, uint8_t frame, int x, int y, const TopArtFigure& figure) {
  const int left = figure.flip ? MASCOT_WIDTH - x - part.width : x;
  draw_art(target, part.frame(frame), TOP_PROP_PALETTE, TOP_ART_SCALE, figure.x + left * TOP_ART_SCALE,
           figure.y + y * TOP_ART_SCALE, figure.flip, 0);
}

// マスコットと小物。静かな表示でもこれだけは繰り返す。
template <typename Target>
void draw_top_art_figure(Target& target, const TopArtTier& tier, const TopArtFigure& figure, uint16_t ms) {
  draw_mascot(target, figure.pose, TOP_ART_SCALE, figure.x, figure.y, figure.flip);
  const TopPropPlacement prop = top_prop(tier.prop);
  if (!prop.part) return;
  draw_top_prop(target, *prop.part, 0, prop.x, prop.y, figure);
  const int glint = tier.prop == TopProp::Medal ? top_glint_frame(ms) : -1;
  if (glint >= 0)
    draw_top_prop(target, TOP_GLINT_PART, static_cast<uint8_t>(glint), prop.x + TOP_GLINT_X, prop.y + TOP_GLINT_Y, figure);
}

template <typename Target>
void draw_top_fireworks(Target& target, uint16_t ms) {
  for (uint8_t index = 0; index < sizeof(TOP_FIREWORKS) / sizeof(TOP_FIREWORKS[0]); ++index) {
    const TopFirework& fw = TOP_FIREWORKS[index];
    if (ms < fw.launchMs) continue;
    const uint32_t t = ms - fw.launchMs;
    if (t < FIREWORK_RISE_MS) {
      // 範囲の下端から開く位置まで昇る。
      const int bottom = TOP_ART_Y + TOP_ART_HEIGHT;
      const int y = bottom - int((bottom - fw.y) * t / FIREWORK_RISE_MS);
      target.fillRect(fw.x - 1, y, 2, 4, 0xffff);
      target.fillRect(fw.x - 1, y + 4, 2, 4, dim565(fw.color, GAIN_LEVELS / 2));
      continue;
    }
    if (t >= FIREWORK_RISE_MS + FIREWORK_BURST_MS) continue;
    const float k = float(t - FIREWORK_RISE_MS) / FIREWORK_BURST_MS;
    if (k < 0.15f) target.fillRect(fw.x - 2, fw.y - 2, 5, 5, 0xffff);
    const float r = fw.radius * top_art_ease_out(k);
    const int fall = int(FIREWORK_FALL_PX * k * k);
    const int size = k < 0.6f ? 3 : 2;
    const uint8_t level = k < 0.6f ? GAIN_LEVELS : static_cast<uint8_t>(std::max(1.0f, GAIN_LEVELS * (1 - k) / 0.4f));
    for (uint8_t i = 0; i < FIREWORK_SPARKS + FIREWORK_INNER_SPARKS; ++i) {
      const bool inner = i >= FIREWORK_SPARKS;
      const uint8_t count = inner ? FIREWORK_INNER_SPARKS : FIREWORK_SPARKS;
      const float angle = TOP_ART_TAU * ((inner ? i - FIREWORK_SPARKS : i) + 0.5f * (index + inner)) / count;
      const float radius = inner ? r * 0.55f : r;
      const int x = fw.x + int(std::lround(std::cos(angle) * radius)) - size / 2;
      const int y = fw.y + int(std::lround(std::sin(angle) * radius)) + fall - size / 2;
      target.fillRect(x, y, size, size, dim565(inner || i % 2 ? fw.color : 0xffff, level));
    }
  }
}

template <typename Target>
void draw_top_streaks(Target& target, uint16_t ms) {
  for (const auto& streak : TOP_STREAKS) {
    if (ms < streak.startMs || ms >= streak.startMs + STREAK_MS) continue;
    // 頭が範囲の左の外から出て、尾が右の外へ抜ける。
    const int travel = TOP_ART_WIDTH + STREAK_HEAD + STREAK_SEGMENTS * STREAK_SEGMENT;
    const int head = TOP_ART_X - STREAK_HEAD + travel * (ms - streak.startMs) / STREAK_MS;
    target.fillRect(head, streak.y - 1, STREAK_HEAD, 3, 0xffff);
    for (int s = 0; s < STREAK_SEGMENTS; ++s)
      target.fillRect(head - (s + 1) * STREAK_SEGMENT, streak.y - (s < 2 ? 1 : 0), STREAK_SEGMENT, s < 2 ? 2 : 1,
                      dim565(STREAK_COLOR, static_cast<uint8_t>(GAIN_LEVELS - s * GAIN_LEVELS / (STREAK_SEGMENTS + 1))));
  }
}

template <typename Target>
void draw_top_sparkles(Target& target, const TopArtTier& tier, uint16_t ms) {
  if (ms < SPARKLE_FROM_MS || ms >= SPARKLE_TO_MS) return;
  // トロフィーの中央（動きの基準の位置）から散る。
  const TopPropPlacement prop = top_prop(tier.prop);
  const int width = prop.part ? prop.part->width : MASCOT_WIDTH, height = prop.part ? prop.part->height : MASCOT_HEIGHT;
  const int ox = TOP_ART_FIGURE_X + (prop.x * 2 + width) * TOP_ART_SCALE / 2;
  const int oy = top_art_rest_y(tier) + (prop.y * 2 + height) * TOP_ART_SCALE / 2;
  const float k = top_art_ease_out(std::min(1.0f, float(ms - SPARKLE_FROM_MS) / SPARKLE_SPREAD_MS));
  const int half = TOP_SPARKLE_PART.width * TOP_SPARKLE_SCALE / 2;
  for (uint8_t i = 0; i < TOP_SPARKLES; ++i) {
    // 左（半周）から上を通って右へ、上半分の向きに並べる。
    const float angle = TOP_ART_TAU / 2 * (1 + (i + 0.5f) / TOP_SPARKLES);
    const int x = ox + int(std::lround(std::cos(angle) * TOP_SPARKLE_RX * k)) - half;
    const int y = oy + int(std::lround(std::sin(angle) * TOP_SPARKLE_RY * k)) - half;
    const uint8_t frame = static_cast<uint8_t>((ms / TOP_SPARKLE_PART.frameMs + i) % TOP_SPARKLE_PART.count);
    draw_art(target, TOP_SPARKLE_PART.frame(frame), TOP_PROP_PALETTE, TOP_SPARKLE_SCALE, x, y, false, 0);
  }
}

template <typename Target>
void draw_top_confetti(Target& target, uint16_t ms) {
  for (uint8_t i = 0; i < TOP_CONFETTI; ++i) {
    const uint32_t h = effect_hash(TOP_ART_SEED, i);
    const uint16_t delay = static_cast<uint16_t>((h >> 8) % 400);
    const uint16_t fall = static_cast<uint16_t>(1100 + (h >> 16) % 400);
    if (ms < delay || ms >= delay + fall) continue;
    // 範囲の上端の外から落ち、少し左右に揺れながら、ときどき裏返って細く見える。
    const int x = TOP_ART_X + int(h % (TOP_ART_WIDTH - CONFETTI_SIZE)) + top_art_wave(ms + i * 37, 400, 2);
    const int y = TOP_ART_Y - CONFETTI_SIZE + int((ms - delay) * (TOP_ART_HEIGHT + CONFETTI_SIZE) / fall);
    const bool edge = (ms / 90 + i) % 3 == 0;
    target.fillRect(x, y, CONFETTI_SIZE, edge ? 1 : CONFETTI_SIZE,
                    CONFETTI_COLORS[i % (sizeof(CONFETTI_COLORS) / sizeof(CONFETTI_COLORS[0]))]);
  }
}

// 範囲の中に、後ろの派手な動き → マスコットと小物 → 前の派手な動きの順に重ねて描く。背景は消さない。
template <typename Target>
void draw_top_art(Target& target, const TopArtTier& tier, const TopArtFrame& f) {
  if (!f.shown) return;
  TopArtClip<Target> clip{target};
  if (f.burst && tier.burst == TopBurst::Fireworks) draw_top_fireworks(clip, f.ms);
  if (f.burst && tier.burst == TopBurst::StreakStars) draw_top_streaks(clip, f.ms);
  draw_top_art_figure(clip, tier, top_art_figure(tier, f.ms), f.ms);
  if (f.burst && tier.burst == TopBurst::StreakStars) draw_top_sparkles(clip, tier, f.ms);
  if (f.burst && tier.burst == TopBurst::Confetti) draw_top_confetti(clip, f.ms);
}

// ②のヘッダーの絵の状態を作る。ResultPagerと同じく実経過時間で進め、ブロッキングしない。
class TopPercentArt {
public:
  // shown: ②（上位%か下位50%）を表示している。celebrating: 通常の終了遷移で初めて開いた②（ResultPager::celebrating）。
  // introProgress: ②の塗りの演出の進行度（1000で終わり）。
  void update(bool shown, bool celebrating, uint16_t introProgress, uint32_t nowMs) {
    if (!shown) {
      // ページを替えたら止める。②に戻ったら静かな表示から始める。
      running_ = false;
      frame_ = {};
      return;
    }
    if (!running_) {
      running_ = true;
      started_ = false;
      burst_ = celebrating;
    }
    if (!celebrating) burst_ = false;
    if (!started_) {
      // 派手な動きは塗りの演出が終わってから始める。それまで絵は出さない。
      if (burst_ && introProgress < 1000) {
        frame_ = {};
        return;
      }
      started_ = true;
      startMs_ = nowMs;
    }
    const uint32_t elapsed = (nowMs - startMs_) / TOP_ART_STEP_MS * TOP_ART_STEP_MS;
    // 派手な動きは1周で終え、あとは絵だけを静かに繰り返す。
    if (elapsed >= TOP_ART_CYCLE_MS) burst_ = false;
    frame_.shown = true;
    frame_.burst = burst_;
    frame_.ms = static_cast<uint16_t>(elapsed % TOP_ART_CYCLE_MS);
  }
  const TopArtFrame& frame() const { return frame_; }

private:
  bool running_ = false, started_ = false, burst_ = false;
  uint32_t startMs_ = 0;
  TopArtFrame frame_{};
};

#endif // TOP_PERCENT_ART_HPP_
