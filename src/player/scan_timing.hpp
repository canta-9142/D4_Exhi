// This file was fully written by AI, so don't ask me...

#ifndef SCAN_TIMING_HPP_
#define SCAN_TIMING_HPP_

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>

constexpr int      SCAN_LINES                    = 512; // RAW80の範囲。物理座標との対応は仮定。
constexpr int      SCAN_ACTIVE_LINES             = 480;
constexpr float    FALLBACK_START_OFFSET_US      = 6750;
constexpr int      FALLBACK_SCAN_MARGIN_LINES    = 40;
constexpr float    SCAN_DEFAULT_PERIOD_US        = 8752;
constexpr int      SCAN_MIN_PERIOD_US            = 6000;
constexpr int      SCAN_MAX_PERIOD_US            = 24000;
constexpr int      SCAN_COARSE_SEARCH_STEP_US    = 80;
constexpr int      SCAN_FINE_SEARCH_STEP_US      = 2;
constexpr float    SCAN_MIN_FIT_QUALITY          = 0.65f;
constexpr float    SCAN_MAX_PHASE_ERROR_FRACTION = 0.35f;
constexpr float    SCAN_PERIOD_UPDATE_GAIN       = 0.002f;
constexpr float    SCAN_PHASE_UPDATE_GAIN        = 0.125f;
constexpr float    SCAN_MAX_PERIOD_CORRECTION_US = 2;
constexpr uint32_t SCAN_MIN_UPDATE_INTERVAL_US   = 5000;
constexpr uint32_t SCAN_MAX_UPDATE_INTERVAL_US   = 250000;
constexpr float    SCAN_MIN_GUARD_US             = 300;
constexpr float    SCAN_JITTER_DIVISOR           = 4;
constexpr float    SCAN_TWO_PI                   = 6.28318530718f;

inline float wrap_phase(float value, float period) {
  value = std::fmod(value, period);
  return value < 0 ? value + period : value;
}

struct ScanSample {
  uint32_t timeUs;
  uint16_t line;
};

inline float phase_difference(float measuredUs, float predictedUs, float periodUs) {
  return wrap_phase(measuredUs - predictedUs + periodUs / 2, periodUs) - periodUs / 2;
}

class ScanTiming {
public:
  float periodUs = SCAN_DEFAULT_PERIOD_US;
  bool locked = false;

  bool fit(const std::array<ScanSample, 64>& samples) {
    unsigned count = 0;
    uint16_t low = UINT16_MAX, high = 0;
    const uint32_t origin = samples[0].timeUs;
    for (const auto& s : samples) if (s.line < SCAN_LINES) {
      ++count;
      low = std::min(low, s.line);
      high = std::max(high, s.line);
    }
    if (count < 32 || high - low < SCAN_LINES / 2) return locked = false;
    float bestPeriod = periodUs, bestQuality = 0, bestPhase = 0;
    const auto score = [&](float period) {
      float cosine = 0, sine = 0;
      for (const auto& s : samples) if (s.line < SCAN_LINES) {
        const float angle = SCAN_TWO_PI * (float(s.line) / SCAN_LINES
                            - float(uint32_t(s.timeUs - origin)) / period);
        cosine += std::cos(angle); sine += std::sin(angle);
      }
      const float quality = std::sqrt(cosine * cosine + sine * sine) / count;
      if (quality > bestQuality) {
        bestQuality = quality;
        bestPeriod = period;
        bestPhase = wrap_phase(std::atan2(sine, cosine) * period / SCAN_TWO_PI, period);
      }
    };
    for (int period = SCAN_MIN_PERIOD_US; period <= SCAN_MAX_PERIOD_US;
         period += SCAN_COARSE_SEARCH_STEP_US) score(period);
    const int coarse = int(bestPeriod);
    const int fineStartUs = std::max(SCAN_MIN_PERIOD_US, coarse - SCAN_COARSE_SEARCH_STEP_US);
    const int fineEndUs = std::min(SCAN_MAX_PERIOD_US, coarse + SCAN_COARSE_SEARCH_STEP_US);
    for (int period = fineStartUs; period <= fineEndUs; period += SCAN_FINE_SEARCH_STEP_US) score(period);
    if (bestQuality < SCAN_MIN_FIT_QUALITY) return locked = false;
    periodUs = bestPeriod;
    referenceUs_ = origin;
    referencePhaseUs_ = bestPhase;
    std::array<float, 64> errors{};
    unsigned i = 0;
    for (const auto& s : samples) if (s.line < SCAN_LINES) {
      const float measured = float(s.line) * periodUs / SCAN_LINES;
      errors[i++] = std::fabs(phase_difference(measured, phase_at(s.timeUs), periodUs));
    }
    std::sort(errors.begin(), errors.begin() + count);
    phaseJitterUs_ = errors[(count - 1) * 95 / 100];
    return locked = true;
  }

  float phase_at(uint32_t timeUs) const {
    return wrap_phase(referencePhaseUs_ + float(uint32_t(timeUs - referenceUs_)), periodUs);
  }

  void observe(const ScanSample& sample) {
    if (sample.line >= SCAN_LINES) return;
    const float predicted = phase_at(sample.timeUs);
    const float measured = float(sample.line) * periodUs / SCAN_LINES;
    if (!locked) {
      referenceUs_ = sample.timeUs;
      referencePhaseUs_ = measured;
      return;
    }
    const float error = phase_difference(measured, predicted, periodUs);
    if (std::fabs(error) > periodUs * SCAN_MAX_PHASE_ERROR_FRACTION) return;
    const uint32_t elapsedUs = sample.timeUs - referenceUs_;
    if (elapsedUs >= SCAN_MIN_UPDATE_INTERVAL_US && elapsedUs < SCAN_MAX_UPDATE_INTERVAL_US) {
      const float correctionUs = std::clamp(-error * periodUs / elapsedUs * SCAN_PERIOD_UPDATE_GAIN,
          -SCAN_MAX_PERIOD_CORRECTION_US, SCAN_MAX_PERIOD_CORRECTION_US);
      periodUs = std::clamp(periodUs + correctionUs, float(SCAN_MIN_PERIOD_US), float(SCAN_MAX_PERIOD_US));
    }
    referenceUs_ = sample.timeUs;
    referencePhaseUs_ = wrap_phase(predicted + error * SCAN_PHASE_UPDATE_GAIN, periodUs);
  }

  float guard_us() const { return std::max(SCAN_MIN_GUARD_US, phaseJitterUs_ / SCAN_JITTER_DIVISOR); }

private:
  uint32_t referenceUs_ = 0;
  float referencePhaseUs_ = 0;
  float phaseJitterUs_ = 2000;
};

struct TransferPlan {
  float periodUs = SCAN_DEFAULT_PERIOD_US;
  float firstScanPhaseUs = 0, startDelayFromScanUs = 0;
  float earliestStartDelayUs = 0, latestStartDelayUs = 0;
  float guardUs = 0, blankingUs = 0;
  bool feasible = false;
};

inline TransferPlan plan_transfer(const ScanTiming& timing, int firstX, int transferWidth,
                                  float columnUs, int blockWidth) {
  TransferPlan plan;
  plan.periodUs = timing.periodUs;
  plan.guardUs = timing.guard_us();
  plan.firstScanPhaseUs = firstX * plan.periodUs / SCAN_LINES;
  // GSCANの0はV-sync開始。前後のblanking配分を決め打ちせず全blanking幅を見込む。
  plan.blankingUs = (SCAN_LINES - SCAN_ACTIVE_LINES) * plan.periodUs / SCAN_LINES;
  plan.earliestStartDelayUs = plan.guardUs + plan.blankingUs;
  plan.latestStartDelayUs = plan.periodUs;
  // 全列が同じパネル周期の前回走査と次回走査の間に入る共通の開始範囲。
  for (int x = 0; x < transferWidth; ++x) {
    const float scanUs = x * plan.periodUs / SCAN_LINES;
    const int firstColumn = x / blockWidth * blockWidth;
    const int endColumn = std::min(firstColumn + blockWidth, transferWidth);
    // 列ごとの進行は仮定せず、ブロック全体の開始・完了を期限に使う。
    plan.earliestStartDelayUs = std::max(plan.earliestStartDelayUs,
        scanUs + plan.guardUs + plan.blankingUs - firstColumn * columnUs);
    plan.latestStartDelayUs = std::min(plan.latestStartDelayUs,
        scanUs + plan.periodUs - plan.guardUs - endColumn * columnUs);
  }
  plan.feasible = timing.locked && plan.latestStartDelayUs >= plan.earliestStartDelayUs;
  const float fallbackStartPhaseUs = wrap_phase(
      (firstX + transferWidth + FALLBACK_SCAN_MARGIN_LINES) * plan.periodUs / SCAN_LINES
          + FALLBACK_START_OFFSET_US, plan.periodUs);
  plan.startDelayFromScanUs = plan.feasible
      ? (plan.earliestStartDelayUs + plan.latestStartDelayUs) / 2
      : wrap_phase(fallbackStartPhaseUs - plan.firstScanPhaseUs, plan.periodUs);
  return plan;
}

#endif // SCAN_TIMING_HPP_
