#ifndef GAME_HPP_
#define GAME_HPP_

#include <Arduino.h>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>
#include <freertos/task.h>
#include "psw.hpp"
#include "node_id.hpp"
#include "packet.hpp"
#ifdef PLAYER_PACKET_DEMO
#include "packet_demo.hpp"
#endif
#include "packet_animation.hpp"
#include "drawing.hpp"

int score = 0;
int remaining = 0;

PacketQueue packetQueue;
PacketAnimation packetAnimation;
#ifdef PLAYER_PACKET_DEMO
PacketDemo packetDemo;
#endif

NodeId selfId = NodeId::None;

QueueHandle_t displayQueue = nullptr;
DrawingTaskContext drawingContext{};

constexpr uint32_t DISPLAY_FRAME_MS = 16;
constexpr BaseType_t DISPLAY_CORE = 1 - ARDUINO_RUNNING_CORE;

bool assign_self_id(uint8_t raw_id) {
  if (raw_id < 1 || raw_id > 8) return false;
  selfId = static_cast<NodeId>(raw_id);
  return true;
}

void publish_display_snapshot() {
  const DisplaySnapshot snapshot{packetAnimation.snapshot(), score, remaining, selfId};
  xQueueOverwrite(displayQueue, &snapshot);
}

void setup() {
  psw_init();
  assign_self_id(1); // TODO: 外部からのID取得へ変更する。

  displayQueue = xQueueCreate(1, sizeof(DisplaySnapshot));
  if (!displayQueue) {
    log_e("Display queue allocation failed");
    return;
  }

#ifdef PLAYER_PACKET_DEMO
  if (!packetDemo.initialize(packetQueue, selfId)) {
    log_e("Demo packet queue full");
    vQueueDelete(displayQueue);
    displayQueue = nullptr;
    return;
  }
  packetAnimation.update(packetQueue, 0, ANIMATION_HEIGHT);
#endif
  publish_display_snapshot();
  drawingContext = {displayQueue, xTaskGetCurrentTaskHandle()};
  if (xTaskCreatePinnedToCore(display_task, "display", 4096, &drawingContext, 1, nullptr, DISPLAY_CORE) != pdPASS) {
    log_e("Display task creation failed");
    vQueueDelete(displayQueue);
    displayQueue = nullptr;
  }
}

void loop() {
  if (!displayQueue) {
    vTaskDelay(pdMS_TO_TICKS(1000));
    return;
  }

  static uint32_t previousFrameMs = millis();
  const uint32_t nowMs = millis();
#ifdef PLAYER_PACKET_DEMO
  packetDemo.update(packetQueue, selfId, nowMs, ulTaskNotifyTake(pdTRUE, 0) != 0);
#endif
  const uint32_t elapsedMs = nowMs - previousFrameMs;
  if (elapsedMs >= DISPLAY_FRAME_MS) {
    previousFrameMs = nowMs;
    packetAnimation.update(packetQueue, elapsedMs, ANIMATION_HEIGHT);
    publish_display_snapshot();
  }
  vTaskDelay(1);
}

#endif // GAME_HPP_
