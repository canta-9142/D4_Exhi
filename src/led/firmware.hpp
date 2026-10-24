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
    ++outputCount;
    if (!strips[i].getPixels() || !strips[i].begin()) {
      controller.reset();
      clear_outputs();
      return;
    }
    strips[i].setBrightness(255);
    strips[i].clear();
    strips[i].show();
  }
}

void setup() {
  Serial.setRxBufferSize(1024);
  Serial.setTxTimeoutMs(0);
  Serial.begin(115200);
  Serial.setDebugOutput(false);
  std::snprintf(nodeUid, sizeof(nodeUid), "%012llX", (unsigned long long)ESP.getEfuseMac());
}

void loop() {
  const uint32_t nowMs = millis();
  const bool configured = controller.poll(Serial, nodeUid, nowMs);
  if (configured) configure_outputs();
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
