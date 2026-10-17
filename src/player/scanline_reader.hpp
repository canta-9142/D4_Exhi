#ifndef SCANLINE_READER_HPP_
#define SCANLINE_READER_HPP_

#include <Arduino.h>
#include <freertos/FreeRTOS.h>
#include <freertos/task.h>
#include "display.hpp"
#include "scan_timing.hpp"

constexpr uint32_t SCAN_READ_GUARD_US = 5000;
constexpr uint32_t SCAN_READ_CLOCK_HZ = 1000000;

inline void wait_microseconds(uint32_t durationUs) {
  const uint32_t startedUs = micros();

  for (;;) {
    const uint32_t elapsedUs = micros() - startedUs;
    if (elapsedUs >= durationUs) return;

    const uint32_t remainingUs = durationUs - elapsedUs;
    if (remainingUs >= 1000) {
      vTaskDelay(1);
    } else {
      delayMicroseconds(remainingUs);
    }
  }
}

class ScanlineReader {
public:
  explicit ScanlineReader(Display& display) : display_(display) {}

  uint32_t remaining_guard_us() const {
    if (!hasRead_) return 0;

    const uint32_t elapsedUs = micros() - lastReadUs_;
    return elapsedUs < SCAN_READ_GUARD_US ? SCAN_READ_GUARD_US - elapsedUs : 0;
  }

  void wait_after_read() const {
    const uint32_t remainingUs = remaining_guard_us();
    if (remainingUs) wait_microseconds(remainingUs);
  }

  ScanSample read() {
    wait_after_read();
    display_.waitDMA();

    auto* bus = display_.getPanel()->getBus();
    bus->setReadClock(SCAN_READ_CLOCK_HZ);
    display_.startWrite();
    bus->writeCommand(0x45, 9);
    bus->wait();
    bus->beginRead(0);

    const uint16_t high = bus->readData(8) & 0xFF;
    const uint16_t low = bus->readData(8) & 0xFF;
    bus->readData(8); // ダミー
    bus->endRead();
    display_.endWrite();
    lastReadUs_ = micros();
    hasRead_ = true;
    
    return {lastReadUs_, uint16_t((high << 8) | low)};
  }

private:
  Display& display_;
  uint32_t lastReadUs_ = 0;
  bool hasRead_ = false;
};

#endif // SCANLINE_READER_HPP_
