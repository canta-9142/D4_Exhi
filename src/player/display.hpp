#ifndef DISPLAY_HPP_
#define DISPLAY_HPP_

#include <Arduino.h>
#include <LovyanGFX.hpp>

constexpr int ANIMATION_X = 120;
constexpr int ANIMATION_Y = 0;
constexpr int ANIMATION_WIDTH = 200;
constexpr int ANIMATION_HEIGHT = 240;

class Display : public lgfx::LGFX_Device {
  lgfx::Bus_SPI bus;
  lgfx::Panel_ST7796 panel;

public:
  Display() {
    auto busCfg = bus.config();
    busCfg.spi_host   = SPI2_HOST;
    busCfg.spi_mode   = 0;
    busCfg.freq_write = 40000000;  // SPI 40MHz
    busCfg.spi_3wire  = false;     // MISOを使用
    busCfg.pin_miso   = 8;
    busCfg.pin_sclk   = 9;
    busCfg.pin_mosi   = 10;
    busCfg.pin_dc     = 11;
    bus.config(busCfg);
    panel.setBus(&bus);

    auto panelCfg = panel.config();
    panelCfg.pin_rst      = 12;
    panelCfg.pin_cs       = 13;
    panelCfg.panel_width  = 320;  // 回転前の寸法
    panelCfg.panel_height = 480;
    panelCfg.readable     = false; // 今回は書き込みのみ
    panelCfg.invert       = false;
    panelCfg.rgb_order    = false;
    panel.config(panelCfg);

    setPanel(&panel);
  }
};

Display lcd;
lgfx::LGFX_Sprite animation(&lcd);

void startup_animation() {
  if (!animation.getBuffer()) return;

  constexpr uint32_t durationMs = 1000;
  constexpr int barLength = 10;
  const int startupX = (lcd.width() - animation.width()) / 2;
  const int startupY = (lcd.height() - animation.height()) / 2;
  animation.setFont(&fonts::Font0);
  animation.setTextSize(2);
  animation.setTextColor(TFT_WHITE, TFT_BLACK);
  const uint32_t startedMs = millis();

  while (true) {
    const uint32_t elapsedMs = millis() - startedMs;
    const uint32_t progressMs = elapsedMs < durationMs ? elapsedMs : durationMs;
    const int filled = progressMs * barLength / durationMs;
    char bar[] = "[..........]";
    for (int i = 0; i < barLength; ++i) {
      bar[i + 1] = i < filled ? '#' : '.';
    }

    animation.fillSprite(TFT_BLACK);
    animation.drawString("STARTING", (ANIMATION_WIDTH - animation.textWidth("STARTING")) / 2, 80);
    animation.drawString(bar, (ANIMATION_WIDTH - animation.textWidth(bar)) / 2, 112);
    animation.setCursor((ANIMATION_WIDTH - animation.textWidth("100%")) / 2, 144);
    animation.printf("%3lu%%", (unsigned long)(progressMs * 100 / durationMs));
    animation.pushSprite(startupX, startupY);
    lcd.waitDMA();

    if (progressMs == durationMs) break;
    delay(1);
  }

  delay(500);  // 100%の表示を0.5秒保持する。

  // 次の画面に起動表示を残さない。
  lcd.fillRect(startupX, startupY, animation.width(), animation.height(), TFT_BLACK);
}

void lcd_init() {
  lcd.init();
  lcd.setRotation(1);  // 横向き480×320
  lcd.setColorDepth(16);
  lcd.fillScreen(TFT_BLACK);

  animation.setColorDepth(16);
  animation.setPsram(false);  // 96,000バイトのスプライトを内蔵RAMに確保
  if (!animation.createSprite(ANIMATION_WIDTH, ANIMATION_HEIGHT)) {
    lcd.fillRect(0, 240, 480, 80, TFT_BLACK);
    lcd.drawString("Sprite allocation failed", 8, 244);
    return;
  }
  animation.setTextColor(TFT_WHITE, TFT_BLACK);
  animation.setTextSize(2);
}

#endif // DISPLAY_HPP_
