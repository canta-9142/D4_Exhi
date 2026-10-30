#ifndef GAIN_ANIMATION_HPP_
#define GAIN_ANIMATION_HPP_

#include <array>
#include <cstddef>
#include <cstdint>
#include "player_status.hpp"

constexpr uint16_t GAIN_DURATION_MS = 1500;
constexpr uint16_t GAIN_FADE_MS     = 500;  // 最後の0.5秒で暗くして消す。
constexpr int      GAIN_RISE        = 20;   // 1.5秒で上へ進む画素数。
constexpr uint8_t  GAIN_LEVELS      = 8;    // 明るさの段階。描き直しの回数を抑える。
constexpr uint8_t  RELAY_GAIN       = 8;    // 中継の点。発信者の点は15・10。

struct GainFloat {
  uint8_t value = 0;
  uint16_t ageMs = 0;
  bool active = false;
};

// 獲得点を、入った時点から下から上へ流して消す。表示専用で、スコアはstatus.scoreをそのまま出す。
// 列ごとに1件を流し、新しい点は最も古い列を上書きするので、同時に出すのは最新の数件まで。
class GainAnimation {
public:
  static constexpr size_t VISIBLE_COUNT = 3;
  using Snapshot = std::array<GainFloat, VISIBLE_COUNT>;

  Snapshot snapshot() const { return floats_; }

  // 経過時間で進めてから、前回より後に入った点を古い順に足す。
  // enabledでなければ流れている点を消し、入った点も捨てる。後から流し直さない。
  void update(const GainBatch& gains, bool enabled, uint32_t elapsedMs) {
    if (!enabled) {
      *this = GainAnimation{};
      return;
    }
    for (auto& gain : floats_) {
      if (!gain.active) continue;
      if (elapsedMs >= uint32_t(GAIN_DURATION_MS - gain.ageMs)) gain = GainFloat{};
      else gain.ageMs += elapsedMs;
    }
    for (uint8_t i = 0; i < gains.count; ++i) {
      floats_[next_] = {gains.values[i], 0, true};
      next_ = (next_ + 1) % VISIBLE_COUNT;
    }
  }

private:
  Snapshot floats_{};
  uint8_t next_ = 0;
};

// 流れ始めからの上昇量（0〜GAIN_RISE）。等速で進める。
inline int gain_offset(const GainFloat& gain) {
  return GAIN_RISE * gain.ageMs / GAIN_DURATION_MS;
}

// 明るさ（1〜GAIN_LEVELS）。最後のGAIN_FADE_MSで段階的に暗くする。
inline uint8_t gain_level(const GainFloat& gain) {
  const uint16_t left = GAIN_DURATION_MS - gain.ageMs;
  if (left >= GAIN_FADE_MS) return GAIN_LEVELS;
  return static_cast<uint8_t>((left * GAIN_LEVELS + GAIN_FADE_MS - 1) / GAIN_FADE_MS);
}

// RGB565の各成分を明るさに合わせて暗くする。
constexpr uint16_t dim565(uint16_t color, uint8_t level) {
  return static_cast<uint16_t>(((color >> 11) * level / GAIN_LEVELS) << 11 |
                               (((color >> 5) & 0x3f) * level / GAIN_LEVELS) << 5 |
                               (color & 0x1f) * level / GAIN_LEVELS);
}

#endif // GAIN_ANIMATION_HPP_
