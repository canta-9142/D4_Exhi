#ifndef FPS_DEMO_HPP_
#define FPS_DEMO_HPP_

#include "display.hpp"
#include "startup_animation.hpp"

uint32_t reportStartedUs = 0;
uint32_t previousFrameStartedUs = 0;
uint32_t frames = 0;
uint32_t maxFrameUs = 0;
uint32_t reports = 0;
uint64_t drawTotalUs = 0;
uint64_t transferTotalUs = 0;

void setup() {
  lcd_init();
  startup_animation();

  lcd.setTextColor(TFT_WHITE, TFT_BLACK);
  lcd.setTextSize(2);
  lcd.drawString("PLAYER", 8, 16);
  lcd.drawString("SPI 80 MHz", 328, 16);
  lcd.drawString("Full:", 328, 38);
  lcd.drawString("   480x320", 328, 60);
  lcd.drawString("Sprite:", 328, 82);
  lcd.drawString("   200x240", 328, 104);
  lcd.drawString("RGB565", 328, 126);
  lcd.drawString("UNCAPPED", 328, 148);
  lcd.drawString("Measuring...", 8, 244);

  reportStartedUs = micros();
}

void loop() {
  if (!sprite.getBuffer()) {
    delay(1000);
    return;
  }

  const uint32_t frameStartedUs = micros();
  if (previousFrameStartedUs != 0) {
    const uint32_t frameUs = frameStartedUs - previousFrameStartedUs;
    if (frameUs > maxFrameUs) maxFrameUs = frameUs;
  }
  previousFrameStartedUs = frameStartedUs;

  // 経過時間で動かすので、FPSが変わっても移動速度は一定。
  const uint32_t elapsedMs = millis();
  sprite.fillSprite(TFT_BLACK);
  sprite.drawRect(0, 0, ANIMATION_WIDTH, ANIMATION_HEIGHT, TFT_WHITE);
  for (int lane = 0; lane < 3; ++lane) {
    const uint32_t phase = (elapsedMs + lane * 600) % 2400;
    const uint32_t travel = phase < 1200 ? phase : 2400 - phase;
    const int x = 4 + travel * (ANIMATION_WIDTH - 72) / 1200;
    const int y = 24 + lane * 72;
    sprite.drawRect(x, y, 64, 32, TFT_WHITE);
    sprite.setCursor(x + 20, y + 8);
    sprite.printf("P%d", lane + 1);
  }
  const uint32_t drawFinishedUs = micros();

  sprite.pushSprite(ANIMATION_X, ANIMATION_Y);
  lcd.waitDMA();  // 転送完了までを計測に含める
  const uint32_t transferFinishedUs = micros();

  drawTotalUs += drawFinishedUs - frameStartedUs;
  transferTotalUs += transferFinishedUs - drawFinishedUs;
  ++frames;

  const uint32_t reportUs = transferFinishedUs - reportStartedUs;
  if (reportUs >= 1000000) {
    const float fps = frames * 1000000.0f / reportUs;
    const float drawMs = drawTotalUs / (frames * 1000.0);
    const float transferMs = transferTotalUs / (frames * 1000.0);

    // アニメーション外も毎秒更新。この負荷はFPSと最大フレーム時間に含まれる。
    lcd.fillRect(0, 240, 480, 80, TFT_BLACK);
    lcd.setCursor(8, 244);
    lcd.printf("FPS %5.1f | Frames %lu", fps, (unsigned long)frames);
    lcd.setCursor(8, 268);
    lcd.printf("Draw %5.2f ms | SPI %5.2f ms", drawMs, transferMs);
    lcd.setCursor(8, 292);
    lcd.printf("Max %5.2f ms | UI %lu", maxFrameUs / 1000.0f,
               (unsigned long)++reports);

    frames = 0;
    drawTotalUs = 0;
    transferTotalUs = 0;
    maxFrameUs = 0;
    reportStartedUs = transferFinishedUs;
  }
}

#endif // FPS_DEMO_HPP_
