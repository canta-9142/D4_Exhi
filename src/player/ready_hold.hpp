#ifndef READY_HOLD_HPP_
#define READY_HOLD_HPP_

#include <algorithm>
#include <cstdint>

class ReadyHold {
public:
  static constexpr uint16_t DURATION_MS = 2000;
  static constexpr uint8_t END_BUTTONS = 0x09;

  void reset() {
    waitingRelease_ = true;
    holding_ = false;
    progressMs_ = 0;
  }

  bool update(uint8_t pressedMask, bool enabled, uint32_t nowMs) {
    if (!enabled) { reset(); return false; }
    const uint8_t ends = pressedMask & END_BUTTONS;
    if (waitingRelease_) {
      if (!ends) waitingRelease_ = false;
      return false;
    }
    if (ends != END_BUTTONS) {
      holding_ = false;
      progressMs_ = 0;
      return false;
    }
    if (!holding_) { holding_ = true; startedMs_ = nowMs; }
    progressMs_ = std::min<uint32_t>(nowMs - startedMs_, DURATION_MS);
    if (progressMs_ < DURATION_MS) return false;
    reset(); // 成立後は両端とも離すまで次の要求を作らない。
    return true;
  }

  uint16_t progress_ms() const { return progressMs_; }

private:
  bool waitingRelease_ = true, holding_ = false;
  uint32_t startedMs_ = 0;
  uint16_t progressMs_ = 0;
};

#endif // READY_HOLD_HPP_
