#ifndef NODE_CONFIG_HPP_
#define NODE_CONFIG_HPP_

#include <cstdint>
#include <LovyanGFX.hpp>
#include "node_id.hpp"

struct NodeVisual {
  char name;
  uint16_t color; // RGB565
};

constexpr NodeVisual nodeVisuals[] = {
  {'?', TFT_BLACK},  // None
  {'A', TFT_RED},    // P1
  {'B', TFT_ORANGE}, // P2
  {'C', TFT_YELLOW}, // P3
  {'D', TFT_GREEN},  // P4
  {'E', TFT_CYAN},   // P5
  {'F', TFT_BLUE},   // P6
  {'G', TFT_PURPLE}, // P7
  {'H', TFT_PINK},   // P8
  {'I', TFT_WHITE},  // B1
  {'J', TFT_GRAY},   // B2
  {'K', TFT_BROWN},  // B3
};

struct NodeLayout {
  NodeId neighbors[4];
};

constexpr NodeLayout nodeLayouts[] = {
  {{NodeId::None, NodeId::None, NodeId::None, NodeId::None}}, // None
  {{NodeId::P2, NodeId::B1, NodeId::B3, NodeId::P8}}, // P1
  {{NodeId::P3, NodeId::B2, NodeId::B1, NodeId::P1}}, // P2
  {{NodeId::P4, NodeId::B2, NodeId::B3, NodeId::P2}}, // P3
  {{NodeId::P5, NodeId::B2, NodeId::B1, NodeId::P3}}, // P4
  {{NodeId::P6, NodeId::B3, NodeId::B1, NodeId::P4}}, // P5
  {{NodeId::P7, NodeId::B1, NodeId::B2, NodeId::P5}}, // P6
  {{NodeId::P8, NodeId::B1, NodeId::B3, NodeId::P6}}, // P7
  {{NodeId::P1, NodeId::B2, NodeId::B3, NodeId::P7}}, // P8
};

#endif // NODE_CONFIG_HPP_
