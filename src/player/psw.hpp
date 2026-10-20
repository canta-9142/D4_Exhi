#ifndef PSW_HPP_
#define PSW_HPP_

#include <Arduino.h>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>

// 画面の左から順。各スイッチはGPIOとGNDの間に接続する。
DRAM_ATTR const uint8_t PSW_PINS[4] = {4, 3, 2, 1};
constexpr uint32_t PSW_DEBOUNCE_MS = 20;

struct PswRawState {
  uint8_t pressedMask = 0;
  uint32_t changedMs[4]{};
};

QueueHandle_t pswQueue = nullptr;
// 初期化後はISRだけが更新し、ゲーム側にはキューでコピーする。
// ponytail: 最新状態だけを保持する。loopが20ms以上停止する構成ではエッジFIFOへ変更する。
PswRawState pswInterruptState{};
uint8_t pswPressedMask = 0;

void ARDUINO_ISR_ATTR psw_interrupt() {
  const uint32_t nowMs = millis();
  for (int i = 0; i < 4; ++i) {
    const uint8_t bit = 1u << i;
    const bool pressed = digitalRead(PSW_PINS[i]) == LOW;
    if (pressed == bool(pswInterruptState.pressedMask & bit)) continue;
    pswInterruptState.pressedMask ^= bit;
    pswInterruptState.changedMs[i] = nowMs;
  }

  BaseType_t taskWoken = pdFALSE;
  xQueueOverwriteFromISR(pswQueue, &pswInterruptState, &taskWoken);
  if (taskWoken) portYIELD_FROM_ISR();
}

bool psw_init() {
  pswQueue = xQueueCreate(1, sizeof(PswRawState));
  if (!pswQueue) return false;

  const uint32_t nowMs = millis();
  for (int i = 0; i < 4; ++i) {
    pinMode(PSW_PINS[i], INPUT_PULLUP);
    if (digitalRead(PSW_PINS[i]) == LOW) pswInterruptState.pressedMask |= 1u << i;
    pswInterruptState.changedMs[i] = nowMs;
  }
  xQueueOverwrite(pswQueue, &pswInterruptState);
  for (const uint8_t pin : PSW_PINS) attachInterrupt(pin, psw_interrupt, CHANGE);
  return true;
}

// 確定した押下状態は保持し、新しく押されたビットだけを返す。
uint8_t psw_update() {
  static PswRawState raw;
  xQueueReceive(pswQueue, &raw, 0);
  const uint32_t nowMs = millis();
  uint8_t pressedEdges = 0;
  for (int i = 0; i < 4; ++i) {
    const uint8_t bit = 1u << i;
    if (!((raw.pressedMask ^ pswPressedMask) & bit)) continue;
    if (nowMs - raw.changedMs[i] < PSW_DEBOUNCE_MS) continue;
    pswPressedMask ^= bit;
    pressedEdges |= pswPressedMask & bit;
  }
  return pressedEdges;
}

#endif // PSW_HPP_
