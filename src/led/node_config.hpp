#ifndef LED_NODE_CONFIG_HPP_
#define LED_NODE_CONFIG_HPP_

#include <cstddef>
#include <cstdint>
#include <cstring>

namespace led {

struct Route {
  const char* node;
  const char* neighbor;
  uint8_t pin;
  uint16_t count;
};

// 配置確定後に各行のLED数を実測値へ差し替える。
constexpr Route ROUTES[] = {
  {"B1", "B1", 0, 1}, {"B1", "P1", 1, 1}, {"B1", "P2", 3, 1},
  {"B1", "P4", 4, 1}, {"B1", "P5", 5, 1}, {"B1", "P6", 6, 1},
  {"B1", "P7", 7, 1},
  {"B2", "B2", 0, 1}, {"B2", "P2", 1, 1}, {"B2", "P3", 3, 1},
  {"B2", "P4", 4, 1}, {"B2", "P6", 5, 1}, {"B2", "P8", 6, 1},
  {"B3", "B3", 0, 1}, {"B3", "P1", 1, 1}, {"B3", "P3", 2, 1},
  {"B3", "P5", 3, 1}, {"B3", "P7", 4, 1}, {"B3", "P8", 5, 1},
  {"P1", "P2", 0, 1}, {"P1", "P8", 1, 1},
  {"P3", "P2", 0, 1}, {"P3", "P4", 1, 1},
  {"P5", "P4", 0, 1}, {"P5", "P6", 1, 1},
  {"P7", "P6", 0, 1}, {"P7", "P8", 1, 1},
};

struct Color {
  uint8_t r = 0;
  uint8_t g = 0;
  uint8_t b = 0;

  void add(Color other) {
    r = saturated(r + other.r);
    g = saturated(g + other.g);
    b = saturated(b + other.b);
  }

private:
  static uint8_t saturated(unsigned value) { return value > 255 ? 255 : value; }
};

inline unsigned node_id(const char* text) {
  if (!text || std::strlen(text) != 2) return 0;
  if (text[0] == 'P' && text[1] >= '1' && text[1] <= '8') return text[1] - '0';
  if (text[0] == 'B' && text[1] >= '1' && text[1] <= '3') return 8 + text[1] - '0';
  return 0;
}

inline Color node_color(unsigned id) {
  // src/player/node_config.hppのRGB565配色と同じ値。
  constexpr uint16_t colors[] = {
    0x0000, 0xF800, 0xFDA0, 0xFFE0, 0x07E0, 0x07FF, 0x001F,
    0x780F, 0xFE19, 0xFFFF, 0x8410, 0x9A60,
  };
  if (id >= sizeof(colors) / sizeof(colors[0])) return {};
  const uint16_t color = colors[id];
  return {static_cast<uint8_t>(((color >> 11) & 31) * 255 / 31),
          static_cast<uint8_t>(((color >> 5) & 63) * 255 / 63),
          static_cast<uint8_t>((color & 31) * 255 / 31)};
}

} // namespace led

#endif // LED_NODE_CONFIG_HPP_
