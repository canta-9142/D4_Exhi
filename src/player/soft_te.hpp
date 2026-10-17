#ifndef SOFT_TE_HPP_
#define SOFT_TE_HPP_

#include "scanline_reader.hpp"
#include "scan_timing.hpp"

// 実機による補正パラメータ、今回のアニメーションデモでの調整により決定した値であるため、他のアニメーションには流用できないであろうと思われる
constexpr uint32_t PACKET_PHASE_CORRECTION_US = 4500;
constexpr float TRANSFER_COLUMN_MARGIN_US = 2;

class PacketTransferTiming {
public:
  explicit PacketTransferTiming(Display& display) : scanReader_(display) {}

  void wait_after_scan_read() const {
    scanReader_.wait_after_read();
  }

  void calibrate() {
    std::array<ScanSample, 64> samples;
    constexpr uint32_t gapsUs[] = {5000, 6700, 9300};
    for (size_t i = 0; i < samples.size(); ++i) {
      samples[i] = scanReader_.read();
      wait_microseconds(gapsUs[i % 3]);
    }
    scanTiming_.fit(samples);
  }

  void sample_scan() {
    const ScanSample sample = scanReader_.read();
    validScan_ = sample.line < SCAN_LINES;
    scanTiming_.observe(sample);
  }

  void wait_for_transfer_start(float measuredColumnUs, unsigned rows, unsigned spans) {
    const float columnUs = estimate_column_time(measuredColumnUs, rows, spans);
    auto plan = plan_transfer(scanTiming_, ANIMATION_X, ANIMATION_WIDTH, columnUs, ANIMATION_WIDTH);
    plan.firstScanPhaseUs = wrap_phase(plan.firstScanPhaseUs + PACKET_PHASE_CORRECTION_US, plan.periodUs);
    
    wait_for_start_phase(plan);
    transferStartedUs_ = micros();
  }

  void finish_transfer() {
    previousColumnUs_ = float(micros() - transferStartedUs_) / ANIMATION_WIDTH;
  }

private:
  ScanlineReader scanReader_;
  ScanTiming scanTiming_;
  uint32_t transferStartedUs_ = 0;
  unsigned previousRows_ = 0, previousSpans_ = 0;
  float previousColumnUs_ = 0;
  bool validScan_ = false;

  float estimate_column_time(float measuredUs, unsigned rows, unsigned spans) {
    float columnUs = std::max(1.0f, measuredUs);
    if (rows == previousRows_ && spans == previousSpans_) {
      columnUs = std::max(columnUs, previousColumnUs_);
    }
    previousRows_ = rows;
    previousSpans_ = spans;
    return columnUs + TRANSFER_COLUMN_MARGIN_US;
  }

  void wait_for_start_phase(const TransferPlan& plan) const {
    uint32_t waitUs = 0;

    if (validScan_) {
      const float startPhaseUs = wrap_phase(plan.firstScanPhaseUs + plan.startDelayFromScanUs,plan.periodUs);
      const float phaseNowUs = scanTiming_.phase_at(micros());
      waitUs = uint32_t(std::ceil(wrap_phase(startPhaseUs - phaseNowUs, plan.periodUs)));
    }

    const uint32_t minimumWaitUs = scanReader_.remaining_guard_us();
    if (validScan_) {
      while (waitUs < minimumWaitUs) {
        waitUs += uint32_t(std::ceil(plan.periodUs));
      }
    } else {
      waitUs = minimumWaitUs;
    }
    wait_microseconds(waitUs);
  }
};

#endif // SOFT_TE_HPP_
