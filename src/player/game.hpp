#ifndef GAME_HPP_
#define GAME_HPP_

#include "display.hpp"

void draw_border() {
  lcd.drawLine( 80,   0,  80, 240, TFT_WHITE);
  lcd.drawLine(360,   0, 360, 240, TFT_WHITE);
  lcd.drawLine(360,  80, 480,  80, TFT_WHITE);
  lcd.drawLine(  0, 240, 480, 240, TFT_WHITE);
}

void draw_immutable() {
  
}

void setup() {
  lcd_init();
  startup_animation();

  draw_border();
  draw_immutable();
}

void loop(){

}

#endif // GAME_HPP_
