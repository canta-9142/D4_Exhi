#ifndef SOFT_TE_HPP_
#define SOFT_TE_HPP_

#include "scanline_reader.hpp"

// 実機調整値。GSCANと画面上の走査位置の対応には追加の位相補正が必要。
constexpr uint32_t DISPLAY_PHASE_CORRECTION_US = 4500;

class DisplayTransferTiming {
public:
  explicit DisplayTransferTiming(Display& display) : reader_(display) {}

  void calibrate() {
    std::array<ScanSample, 64> samples;
    constexpr uint32_t gaps[] = {5000, 6700, 9300};
    for (size_t i = 0; i < samples.size(); ++i) {
      samples[i] = reader_.read();
      wait_microseconds(gaps[i % 3]);
    }
    timing_.fit(samples);
    calibrated_ = true;
  }

  void sample() {
    if (!calibrated_) return;
    const auto sample = reader_.read();
    valid_ = sample.line < SCAN_LINES;
    timing_.observe(sample);
  }

  template <size_t N>
  void wait_for_start(const std::array<ScanTransferBlock, N>& blocks, unsigned count) const {
    if (!calibrated_) return;
    auto plan = plan_transfer(timing_, blocks, count);
    const uint32_t minimum = reader_.remaining_guard_us();
    uint32_t waitUs = minimum;
    if (valid_) {
      const float phase = wrap_phase(plan.firstScanPhaseUs + plan.startDelayFromScanUs +
                                     DISPLAY_PHASE_CORRECTION_US, plan.periodUs);
      waitUs = uint32_t(std::ceil(wrap_phase(phase - timing_.phase_at(micros()), plan.periodUs)));
      while (waitUs < minimum) waitUs += uint32_t(std::ceil(plan.periodUs));
    }
    wait_microseconds(waitUs);
  }

private:
  ScanlineReader reader_;
  ScanTiming timing_;
  bool calibrated_ = false, valid_ = false;
};

#endif // SOFT_TE_HPP_
