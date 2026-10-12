#ifndef GAME_HPP_
#define GAME_HPP_

#include <Arduino.h>
#include <LovyanGFX.hpp>
#include "io_config.hpp"
#include "node_config.hpp"
#include "display.hpp"

int score = 0;
int remaining = 0;

NodeId selfId = NodeId::None;
const NodeLayout* layout = nullptr;

bool assign_self_id(uint8_t raw_id) {
  if (raw_id < 1 || raw_id > 8) return false;

  selfId = static_cast<NodeId>(raw_id);
  layout = &nodeLayouts[raw_id];

  return true;
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

void draw_neighbor_buttons() {
  if (!layout) return;

  for (int i = 0; i < 4; ++i) {
    const NodeId id = layout->neighbors[i];
    if (id == NodeId::None) continue;
    draw_button(i * 120, 240, id);
  }
}

void draw_info() {
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
    drawn = true;
  }

  char number[5];
  if (!drawn || score != previousScore) {
    if (snprintf(number, sizeof(number), "%4d", score) > 4) {
      strcpy(number, "####");
    }
    lcd.drawString(number, 366, 121);
    previousScore = score;
  }

  if (!drawn || remaining != previousRemaining) {
    if (snprintf(number, sizeof(number), "%4d", remaining) > 4) {
      strcpy(number, "####");
    }
    lcd.drawString(number, 366, 174);
    previousRemaining = remaining;
  }

  lcd.endWrite();
}

// coordinates are relative to the animation sprite
void draw_packet_card(int y, NodeId destination, int packet_id) {
  constexpr int h = 60;
  const auto& visual = nodeVisuals[static_cast<int>(destination)];

  animation.fillRect(0, y, ANIMATION_WIDTH, h, TFT_LIGHTGRAY);
  animation.fillRect(0, y, 40, h, visual.color);
  animation.drawRect(0, y, ANIMATION_WIDTH, h, TFT_WHITE);
  animation.setTextColor(TFT_BLACK, TFT_LIGHTGRAY);
  animation.setTextSize(4);
  animation.drawChar(visual.name, 80, y + 14);
  animation.setTextSize(2);
  animation.setCursor(ANIMATION_WIDTH - 52, y + h - 18);
  animation.printf("#%3d", packet_id);
}

void setup() {
  lcd_init();
  io_init();
  startup_animation();

  assign_self_id(3); // TODO: 外部からのID取得へ変更する。

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

  lcd.drawRect(120, 0, 200, 240, TFT_WHITE);

  draw_button(360, 0, selfId);
  draw_neighbor_buttons();
  draw_info();
  draw_packet_card(170, NodeId::P1, 1);
  animation.pushSprite(ANIMATION_X, ANIMATION_Y);
}

void loop(){
  
}

#endif // GAME_HPP_
