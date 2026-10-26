#ifndef LED_FIRMWARE_HPP_
#define LED_FIRMWARE_HPP_

#include <Arduino.h>
#include <Adafruit_NeoPixel.h>
#include "controller.hpp"

led::Controller controller;
Adafruit_NeoPixel strips[led::Controller::MAX_STRIPS];
size_t outputCount = 0;
char nodeUid[13];
uint32_t renderedMs = 0;

bool clear_startup_outputs() {
  unsigned maxPin = 0;
  for (const led::Route& route : led::ROUTES) {
    if (route.pin > maxPin) maxPin = route.pin;
  }
  for (unsigned pin = 0; pin <= maxPin; ++pin) {
    uint16_t count = 0;
    for (const led::Route& route : led::ROUTES) {
      if (route.pin == pin && route.count > count) count = route.count;
    }
    if (!count) continue;
    // NODEが未確定なので、各GPIOの最長のテープまで消灯する。
    Adafruit_NeoPixel output(count, pin, NEO_GRB + NEO_KHZ800);
    if (!output.getPixels() || !output.begin()) return false;
    output.clear();
    output.show();
  }
  return true;
}

void clear_outputs() {
  for (size_t i = 0; i < outputCount; ++i) {
    strips[i].clear();
    strips[i].show();
    strips[i].setPin(-1);
    strips[i].updateLength(0);
  }
  outputCount = 0;
}

void configure_outputs() {
  clear_outputs();
  for (size_t i = 0; i < controller.strip_count(); ++i) {
    const led::Route& route = controller.route(i);
    strips[i].updateType(NEO_GRB + NEO_KHZ800);
    strips[i].updateLength(route.count);
    strips[i].setPin(route.pin);
    if (!strips[i].getPixels() || !strips[i].begin()) {
      controller.output_failed();
      strips[i].setPin(-1);
      strips[i].updateLength(0);
      clear_outputs();
      return;
    }
    ++outputCount;
    strips[i].setBrightness(255);
    strips[i].clear();
    strips[i].show();
  }
}

void setup() {
  while (!clear_startup_outputs()) delay(10);
  Serial.setRxBufferSize(1024);
  Serial.setTxTimeoutMs(0);
  Serial.begin(115200);
  Serial.setDebugOutput(false);
  std::snprintf(nodeUid, sizeof(nodeUid), "%012llX", (unsigned long long)ESP.getEfuseMac());
}

void loop() {
  const bool configured = controller.poll(Serial, nodeUid, millis(), configure_outputs);
  const uint32_t nowMs = millis();
  if (configured || nowMs - renderedMs >= led::FRAME_MS) {
    renderedMs = nowMs;
    for (size_t strip = 0; strip < outputCount; ++strip) {
      for (uint16_t pixel = 0; pixel < strips[strip].numPixels(); ++pixel) {
        const led::Color color = controller.pixel(strip, pixel, nowMs);
        strips[strip].setPixelColor(pixel, color.r, color.g, color.b);
      }
      strips[strip].show();
    }
  }
  delay(1);
}

#endif // LED_FIRMWARE_HPP_
