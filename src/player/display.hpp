#ifndef DISPLAY_HPP_
#define DISPLAY_HPP_

#include <Arduino.h>
#include <LovyanGFX.hpp>

constexpr int ANIMATION_X = 120;
constexpr int ANIMATION_Y = 0;
constexpr int ANIMATION_WIDTH = 200;
constexpr int ANIMATION_HEIGHT = 240;

class ColumnPanel : public lgfx::Panel_ST7796 {
public:
  // MADCTLのパラメータMV(Row/Column Exchange)を切り替え、MV=1ならcolumn優先
  void setColumnOrder(bool enabled) {
    uint8_t mode = getMadCtl(_internal_rotation);
    if (enabled) mode &= ~MAD_MV;
    write_command(CMD_MADCTL);
    writeData(mode | (_cfg.rgb_order ? MAD_RGB : MAD_BGR), 1);
    _xs = _xe = _ys = _ye = INT16_MAX;
  }

  // 列優先のDMA転送
  void pushColumnBlockDMA(int x, int y, int width, int height, const lgfx::swap565_t* data) {
    // MV=0ではYをcolumn、Xをrowとして指定。各列のYが先に進む。
    setWindow(y, x, y + height - 1, x + width - 1);
    _bus->writeBytes(reinterpret_cast<const uint8_t*>(data),
                     width * height * sizeof(*data), true, true);
  }
};

class Display : public lgfx::LGFX_Device {
  lgfx::Bus_SPI bus;
  ColumnPanel panel;

public:
  void beginColumnWrite() {
    startWrite();
    panel.setColumnOrder(true);
  }

  void endColumnWrite() {
    waitDMA();
    panel.setColumnOrder(false);
    endWrite();
  }

  void pushColumnBlockDMA(int x, int y, int width, int height,
                         const lgfx::swap565_t* data) {
    panel.pushColumnBlockDMA(x, y, width, height, data);
  }

  Display() {
    auto busCfg = bus.config();
    busCfg.spi_host   = SPI2_HOST;
    busCfg.spi_mode   = 0;
    busCfg.freq_write = 80000000;  // SPI 80MHz
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
    panelCfg.readable     = false; // 画像の読み戻しは使わない（GSCANは直接読む）
    panelCfg.invert       = false;
    panelCfg.rgb_order    = false;
    panel.config(panelCfg);

    setPanel(&panel);
  }
};

Display lcd;
lgfx::LGFX_Sprite sprite(&lcd);

void lcd_init() {
  lcd.init();
  lcd.setRotation(1);  // 横向き480×320
  lcd.setColorDepth(16);
  lcd.fillScreen(TFT_BLACK);

  sprite.setColorDepth(16);
  sprite.setPsram(false);  // 96,000バイトのスプライトを内蔵RAMに確保
  if (!sprite.createSprite(ANIMATION_WIDTH, ANIMATION_HEIGHT)) {
    lcd.fillRect(0, 240, 480, 80, TFT_BLACK);
    lcd.drawString("Sprite allocation failed", 8, 244);
    return;
  }
  sprite.setTextColor(TFT_WHITE, TFT_BLACK);
  sprite.setTextSize(2);
}

#endif // DISPLAY_HPP_
