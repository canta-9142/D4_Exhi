#pragma once

#include <Arduino.h>
#include <algorithm>
#include <array>
#include <cassert>
#include <vector>

constexpr unsigned NEO_GRB = 0x52;
constexpr unsigned NEO_KHZ800 = 0;

// MCU再起動後も残っているLEDの表示を模擬する。
inline std::array<std::vector<uint8_t>, 8> retainedPixels;
inline bool failNextBegin = false;
inline bool failNextAllocation = false;
inline unsigned showsBeforeSerial = 0;

class Adafruit_NeoPixel {
public:
  Adafruit_NeoPixel(uint16_t count = 0, int16_t pin = -1,
                    unsigned type = NEO_GRB + NEO_KHZ800) : pin_(pin) {
    updateType(type);
    updateLength(count);
  }
  bool begin() {
    if (failNextBegin) {
      failNextBegin = false;
      return false;
    }
    return begun_ = true;
  }
  void updateType(unsigned type) { assert(type == NEO_GRB + NEO_KHZ800); }
  void updateLength(uint16_t count) {
    if (count && failNextAllocation) {
      failNextAllocation = false;
      pixels_.clear();
    } else {
      pixels_.assign(count * 3, 255);
    }
  }
  void setPin(int16_t pin) { pin_ = pin; }
  void setBrightness(uint8_t) {}
  uint8_t* getPixels() { return pixels_.empty() ? nullptr : pixels_.data(); }
  uint16_t numPixels() const { return pixels_.size() / 3; }
  void clear() { std::fill(pixels_.begin(), pixels_.end(), 0); }
  void setPixelColor(uint16_t index, uint8_t r, uint8_t g, uint8_t b) {
    pixels_.at(index * 3) = r;
    pixels_.at(index * 3 + 1) = g;
    pixels_.at(index * 3 + 2) = b;
  }
  void show() {
    assert(begun_ && pin_ >= 0);
    retainedPixels.at(pin_) = pixels_;
    if (!Serial.started) ++showsBeforeSerial;
  }
private:
  int16_t pin_;
  bool begun_ = false;
  std::vector<uint8_t> pixels_;
};
