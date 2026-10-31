#pragma once

// ホストテストでnode_config.hppを読むための代替。node_config.hppが使う色の名前だけを定義する。
// 値はLovyanGFXのRGB565に合わせているが、テストは色の値そのものには依存しない。
#include <cstdint>

constexpr uint16_t TFT_BLACK = 0x0000;
constexpr uint16_t TFT_RED = 0xF800;
constexpr uint16_t TFT_ORANGE = 0xFDA0;
constexpr uint16_t TFT_YELLOW = 0xFFE0;
constexpr uint16_t TFT_GREEN = 0x07E0;
constexpr uint16_t TFT_CYAN = 0x07FF;
constexpr uint16_t TFT_BLUE = 0x001F;
constexpr uint16_t TFT_PURPLE = 0x780F;
constexpr uint16_t TFT_PINK = 0xFE19;
constexpr uint16_t TFT_WHITE = 0xFFFF;
constexpr uint16_t TFT_GRAY = 0x8410;
constexpr uint16_t TFT_BROWN = 0x9A60;
