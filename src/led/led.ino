#include <Adafruit_NeoPixel.h>
#include <math.h>

constexpr int LED_COUNT = 60;
constexpr int TAIL_LENGTH = 20;
constexpr int FRAME_MS = 20;
constexpr int COLOR_COUNT = 8;

Adafruit_NeoPixel leds(LED_COUNT, 1, NEO_GRB + NEO_KHZ800);

void setup() {
  leds.begin();
  leds.setBrightness(255);
  leds.clear();
  leds.show();
}

void loop() {
  static int colorIndex = 0;
  const uint16_t hue = colorIndex * (65536 / COLOR_COUNT);

  // TAIL_LENGTH個で先頭255から末尾1まで減衰する公比。
  const float fadeRatio = TAIL_LENGTH > 1
    ? powf(1.0f / 255.0f, 1.0f / (TAIL_LENGTH - 1))
    : 1.0f;

  for (int head = 0; head < LED_COUNT + TAIL_LENGTH; ++head) {
    leds.clear();

    for (int offset = 0; offset < TAIL_LENGTH; ++offset) {
      const int pixel = head - offset;
      if (pixel < 0 || pixel >= LED_COUNT) continue;

      const uint8_t intensity = lroundf(255.0f * powf(fadeRatio, offset));
      leds.setPixelColor(pixel, leds.ColorHSV(hue, 255, intensity));
    }

    leds.show();
    delay(FRAME_MS);
  }

  colorIndex = (colorIndex + 1) % COLOR_COUNT;
}
