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
#include "gain_animation.hpp"
#include "playing_effects.hpp"
#include "drawing.hpp"

PacketQueue packetQueue;
PacketAnimation packetAnimation;
#ifdef PLAYER_PACKET_DEMO
PacketDemo packetDemo;
#else
ServerLink serverLink;
char nodeUid[13]{};
FinishedAnimation finishedAnimation;
CountdownAnimation countdownAnimation;
GainAnimation gainAnimation;
PlayingEffects playingEffects;
ResultPager resultPager;
// スナップショットの発行までに押された右端ボタン。発行の間隔より短い押下も取りこぼさない。
uint8_t resultPageEdges = 0;
#endif

NodeId selfId = NodeId::None;

QueueHandle_t displayQueue = nullptr;
DrawingTaskContext drawingContext{};
bool displayReady = false;

constexpr uint32_t DISPLAY_FRAME_MS = 16;
constexpr BaseType_t DISPLAY_CORE = 1 - ARDUINO_RUNNING_CORE;

void publish_display_snapshot(uint32_t elapsedMs) {
#ifdef PLAYER_PACKET_DEMO
  const GamePhase phase = GamePhase::Playing;
  const bool synced = true;
  const ControlMode mode = ControlMode::Manual;
  const PlayerStatus status{0, 0, false, true};
  const bool awaitingStatus = false;
  const uint16_t readyHoldMs = 0;
  const bool readyPending = false;
#else
  const GamePhase phase = serverLink.phase();
  const bool synced = serverLink.fifo_synced();
  const ControlMode mode = serverLink.mode();
  const PlayerStatus status = serverLink.status();
  const bool awaitingStatus = serverLink.awaiting_status();
  const uint16_t readyHoldMs = serverLink.ready_progress_ms();
  const bool readyPending = serverLink.ready_pending();
#endif
  DisplaySnapshot snapshot{packetAnimation.snapshot(), status, selfId,
                                 pswPressedMask, phase, mode, synced, awaitingStatus, readyHoldMs, readyPending};
#ifndef PLAYER_PACKET_DEMO
  snapshot.preview = serverLink.preview();
  snapshot.summary = serverLink.summary();
  const bool finished = !snapshot.preview && snapshot.phase == GamePhase::Finished;
  const bool resultReady = finished && finished_state(snapshot) == FinishedState::Ready;
  resultPager.update(snapshot.summary, finished, resultReady, resultPageEdges & ResultPager::NEXT_BUTTON, millis());
  resultPageEdges = 0;
  snapshot.resultPage = resultPager.page();
  snapshot.topProgress = resultPager.progress();
  snapshot.finishedProgress = finishedAnimation.update(snapshot.summary,
      resultReady && snapshot.resultPage == ResultPage::Score, millis());
  snapshot.previewLit = millis() / 500 % 2 == 0;
  snapshot.countdownGo = countdownAnimation.update(snapshot.status.countdown, countdown_enabled(snapshot), millis());
  // 画面の種類が決まってから流すかを判定する。流せない間に入った点は捨て、後から流し直さない。
  const bool gainsEnabled = gains_enabled(snapshot);
  gainAnimation.update(serverLink.take_gains(), gainsEnabled, elapsedMs);
  snapshot.gains = gainAnimation.snapshot();
  // 炎・マスコット・コンボと順位の演出。流せない画面では状態を消し、戻ったときに画面を離れていた間の変化を演出しない。
  playingEffects.update(snapshot.status.combo, snapshot.status.rank, gainsEnabled, millis());
  snapshot.effects = playingEffects.frame();
  snapshot.rankFlash = playingEffects.rank_flash();
#else
  (void)elapsedMs;
#endif
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
  publish_display_snapshot(0);
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
  bool sendButtons = true;
#ifdef PLAYER_PACKET_DEMO
  packetDemo.update(packetQueue, selfId, nowMs);
#else
  if (serverLink.poll(Serial, nodeUid, packetQueue, selfId, nowMs)) {
    packetAnimation = PacketAnimation{};
  }
  serverLink.update_ready(Serial, pswPressedMask, nowMs);
  // ボタンでSENDするのはPlayingだけ。Finishedの押下は結果のページ送りにだけ使い、両者を混ぜない。
  // Playingの最後に押したボタンで、終了直後のページを送らないようにする。
  if (serverLink.phase() == GamePhase::Finished) resultPageEdges |= pressedEdges;
  sendButtons = serverLink.phase() == GamePhase::Playing;
#endif
  if (pressedEdges && sendButtons && packetQueue.size() != 0) {
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
    publish_display_snapshot(elapsedMs);
  }
  vTaskDelay(1);
}

#endif // GAME_HPP_
