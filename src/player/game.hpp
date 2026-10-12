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

bool assign_self_id(uint8_t raw_id) {
  if (raw_id < 1 || raw_id > 8) return false;

  selfId = static_cast<NodeId>(raw_id);
  layout = &nodeLayouts[raw_id];

  return true;
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
  draw_button(360, 0, selfId);
  draw_neighbor_buttons();
}

void loop(){

}

#endif // GAME_HPP_
