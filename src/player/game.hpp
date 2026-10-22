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
#else
#include "server_link.hpp"
#if !ARDUINO_USB_CDC_ON_BOOT
#error "Enable USB CDC On Boot when building the player firmware"
#endif
#endif
#include "packet_animation.hpp"
#include "drawing.hpp"

int score = 0;
int remaining = 0;

PacketQueue packetQueue;
PacketAnimation packetAnimation;
#ifdef PLAYER_PACKET_DEMO
PacketDemo packetDemo;
#else
ServerLink serverLink;
char nodeUid[13]{};
#endif

NodeId selfId = NodeId::None;

QueueHandle_t displayQueue = nullptr;
DrawingTaskContext drawingContext{};
bool displayReady = false;

constexpr uint32_t DISPLAY_FRAME_MS = 16;
constexpr BaseType_t DISPLAY_CORE = 1 - ARDUINO_RUNNING_CORE;

void publish_display_snapshot() {
  const DisplaySnapshot snapshot{packetAnimation.snapshot(), score, remaining, selfId, pswPressedMask};
  xQueueOverwrite(displayQueue, &snapshot);
}

void setup() {
#ifndef PLAYER_PACKET_DEMO
  // Hardware CDCはRXキューが満杯になるとバイトを破棄するため、最大1行分を確保する。
  Serial.setRxBufferSize(ServerLink::MAX_LINE_BYTES + 2);
  Serial.setTxTimeoutMs(0);
  Serial.begin(115200);
  Serial.setDebugOutput(false);
  snprintf(nodeUid, sizeof(nodeUid), "%012llX", (unsigned long long)ESP.getEfuseMac());
#endif
  if (!psw_init()) {
    log_e("Switch queue allocation failed");
    return;
  }
#ifdef PLAYER_PACKET_DEMO
  selfId = NodeId::P1;
#endif

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
  if (ulTaskNotifyTake(pdTRUE, 0) != 0) displayReady = true;
  // 初回描画の完了前にはHELLOも送らず、描画失敗を疎通成功として扱わない。
  if (!displayReady) {
    vTaskDelay(1);
    return;
  }
  const uint8_t pressedEdges = psw_update();
#ifdef PLAYER_PACKET_DEMO
  packetDemo.update(packetQueue, selfId, nowMs);
#else
  if (serverLink.poll(Serial, nodeUid, packetQueue, selfId, nowMs)) {
    packetAnimation = PacketAnimation{};
  }
#endif
  if (pressedEdges && packetQueue.size() != 0) {
    for (int i = 0; i < 4; ++i) {
      if (!(pressedEdges & (1u << i))) continue;
      const NodeId to = nodeLayouts[static_cast<int>(selfId)].neighbors[i];
      if (to == NodeId::None) continue;
#ifdef PLAYER_PACKET_DEMO
      log_i("Demo send: pid=%u to=%u", packetQueue.at(0)->id, static_cast<unsigned>(to));
      packetQueue.pop();
#else
      serverLink.send(Serial, packetQueue, to, nowMs);
#endif
      break; // 同時押しでも先頭パケットを1件だけ扱う。
    }
  }
  const uint32_t elapsedMs = nowMs - previousFrameMs;
  if (elapsedMs >= DISPLAY_FRAME_MS) {
    previousFrameMs = nowMs;
    packetAnimation.update(packetQueue, elapsedMs, ANIMATION_HEIGHT);
    publish_display_snapshot();
  }
  vTaskDelay(1);
}

#endif // GAME_HPP_
