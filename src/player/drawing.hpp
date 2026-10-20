#ifndef DRAWING_HPP_
#define DRAWING_HPP_

#include <Arduino.h>
#include <LovyanGFX.hpp>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>
#include <freertos/task.h>
#include <type_traits>
#include "node_config.hpp"
#include "packet_animation.hpp"
#include "display.hpp"
#include "packet_renderer.hpp"

struct DisplaySnapshot {
  PacketAnimation::Snapshot cards;
  int score;
  int remaining;
  NodeId selfId;
  uint8_t pressedMask;
};

static_assert(std::is_trivially_copyable<DisplaySnapshot>::value, "FreeRTOS queues copy snapshots as bytes");

struct DrawingTaskContext {
  QueueHandle_t snapshots;
  TaskHandle_t readyTask;
};

PacketRenderer packetRenderer{lcd, sprite};

void startup_animation() {
  if (!sprite.getBuffer()) return;

  constexpr uint32_t durationMs = 1000;
  constexpr int barLength = 10;
  const int startupX = (lcd.width() - sprite.width()) / 2;
  const int startupY = (lcd.height() - sprite.height()) / 2;
  sprite.setFont(&fonts::Font0);
  sprite.setTextSize(2);
  sprite.setTextColor(TFT_WHITE, TFT_BLACK);
  const uint32_t startedMs = millis();

  while (true) {
    const uint32_t elapsedMs = millis() - startedMs;
    const uint32_t progressMs = elapsedMs < durationMs ? elapsedMs : durationMs;
    const int filled = progressMs * barLength / durationMs;
    char bar[] = "[..........]";
    for (int i = 0; i < barLength; ++i) {
      bar[i + 1] = i < filled ? '#' : '.';
    }

    sprite.fillSprite(TFT_BLACK);
    sprite.drawString("STARTING", (ANIMATION_WIDTH - sprite.textWidth("STARTING")) / 2, 80);
    sprite.drawString(bar, (ANIMATION_WIDTH - sprite.textWidth(bar)) / 2, 112);
    sprite.setCursor((ANIMATION_WIDTH - sprite.textWidth("100%")) / 2, 144);
    sprite.printf("%3lu%%", (unsigned long)(progressMs * 100 / durationMs));
    sprite.pushSprite(startupX, startupY);
    lcd.waitDMA();

    if (progressMs == durationMs) break;
    delay(1);
  }
  delay(500);
  lcd.fillRect(0, 0, lcd.width(), lcd.height(), TFT_BLACK);
  sprite.fillSprite(TFT_BLACK);
}

void draw_button(int x, int y, NodeId id, bool pressed = false) {
  constexpr int w = 120 - 1;
  constexpr int h = 80 - 1;

  const auto& visual = nodeVisuals[static_cast<int>(id)];

  uint16_t back_color = pressed ? TFT_BLACK : visual.color;
  uint16_t fore_color = pressed ? visual.color : TFT_BLACK;

  lcd.fillRect(x + 1, y + 1, w, h, back_color);
  lcd.setTextColor(fore_color, back_color);
  lcd.setTextSize(6);
  lcd.drawChar(visual.name, x + 42, y + 16);
}

void draw_neighbor_buttons(NodeId id, uint8_t pressedMask, uint8_t changedMask = 0x0f) {
  const auto& layout = nodeLayouts[static_cast<int>(id)];

  for (int i = 0; i < 4; ++i) {
    if (!(changedMask & (1u << i))) continue;
    const NodeId neighbor = layout.neighbors[i];
    if (neighbor == NodeId::None) continue;
    draw_button(i * 120, 240, neighbor, pressedMask & (1u << i));
  }
}

void draw_info_number(int value, int y) {
  char number[5];
  if (snprintf(number, sizeof(number), "%4d", value) > 4) {
    strcpy(number, "####");
  }
  lcd.drawString(number, 366, y);
}

void draw_info(int score, int remaining) {
  static bool drawn = false;
  static int previousScore = 0;
  static int previousRemaining = 0;

  if (drawn && score == previousScore && remaining == previousRemaining) return;

  lcd.setFont(&fonts::Font0);
  lcd.setTextSize(3);
  lcd.setTextColor(TFT_WHITE, TFT_BLACK);
  lcd.setTextDatum(lgfx::textdatum_t::top_left);
  lcd.setTextPadding(0);

  lcd.startWrite();

  if (!drawn) {
    lcd.drawString("pt", 438, 121);
    lcd.drawString("s", 438, 174);
  }

  if (!drawn || score != previousScore) {
    draw_info_number(score, 121);
    previousScore = score;
  }

  if (!drawn || remaining != previousRemaining) {
    draw_info_number(remaining, 174);
    previousRemaining = remaining;
  }

  lcd.endWrite();
  drawn = true;
}

void display_task(void* argument) {
  const auto& context = *static_cast<const DrawingTaskContext*>(argument);
  lcd_init();
  startup_animation();

  lcd.drawLine(360,   0, 360, 240, TFT_WHITE);
  lcd.drawLine(360,  80, 479,  80, TFT_WHITE);
  lcd.drawLine(  0, 240, 479, 240, TFT_WHITE);
  lcd.drawLine(120, 240, 120, 319, TFT_WHITE);
  lcd.drawLine(240, 240, 240, 319, TFT_WHITE);
  lcd.drawLine(360, 240, 360, 319, TFT_WHITE);

  lcd.setTextColor(TFT_WHITE, TFT_BLACK);
  lcd.setTextSize(3);
  lcd.drawChar('3', 51,  28);
  lcd.drawChar('2', 51, 108);
  lcd.drawChar('1', 51, 188);

  packetRenderer.begin();
  xTaskNotifyGive(context.readyTask);

  NodeId previousSelfId = NodeId::None;
  uint8_t previousPressedMask = 0;
  for (;;) {
    DisplaySnapshot snapshot;
    if (xQueueReceive(context.snapshots, &snapshot, portMAX_DELAY) != pdTRUE) continue;
    if (snapshot.selfId != previousSelfId) {
      draw_button(360, 0, snapshot.selfId);
      draw_neighbor_buttons(snapshot.selfId, snapshot.pressedMask);
      previousSelfId = snapshot.selfId;
    } else if (snapshot.pressedMask != previousPressedMask) {
      draw_neighbor_buttons(snapshot.selfId, snapshot.pressedMask,
                            snapshot.pressedMask ^ previousPressedMask);
    }
    previousPressedMask = snapshot.pressedMask;
    draw_info(snapshot.score, snapshot.remaining);
    packetRenderer.draw(snapshot.cards);
    vTaskDelay(1);
  }
}

#endif // DRAWING_HPP_
