#ifndef TUTORIAL_HINT_HPP_
#define TUTORIAL_HINT_HPP_

#include <cstdint>
#include "display_layout.hpp"
#include "node_config.hpp"
#include "player_status.hpp"

// 練習のヒントの帯と、Botの混雑の判定。LCDに依存しない純粋な関数で、ゲーム側と描画側の両方が使う。
// 隣接関係はnode_config.hppのnodeLayoutsから求め、サーバーへは何も問い合わせない。

// 帯の枠の見え方。既定値（0）はつながらない。
enum class HintCell : uint8_t { Unlinked, Linked, Lit };

// 1つのBotのボタンの帯。cells[i]はノードA〜H（P1〜P8）の枠。
struct HintBand {
  HintCell cells[HINT_CELLS]{};
  bool operator==(const HintBand& other) const {
    for (int i = 0; i < HINT_CELLS; ++i)
      if (cells[i] != other.cells[i]) return false;
    return true;
  }
  bool operator!=(const HintBand& other) const { return !(*this == other); }
};

// 左から2番目・3番目のボタンの帯（bands[0]が左から2番目）。
struct TutorialHint {
  HintBand bands[HINT_BUTTONS]{};
  bool operator==(const TutorialHint& other) const {
    for (int i = 0; i < HINT_BUTTONS; ++i)
      if (bands[i] != other.bands[i]) return false;
    return true;
  }
  bool operator!=(const TutorialHint& other) const { return !(*this == other); }
};

constexpr bool is_player_node(NodeId id) { return id >= NodeId::P1 && id <= NodeId::P8; }
constexpr bool is_bot_node(NodeId id) { return id >= NodeId::B1 && id <= NodeId::B3; }
// 帯のi番目の枠のノード（0がA）。
constexpr NodeId hint_cell_node(int cell) { return static_cast<NodeId>(static_cast<int>(NodeId::P1) + cell); }

// 自ノードの下部に並ぶBot（slot 0が左から2番目、1が左から3番目のボタン）。自ノードがプレイヤーでなければNone。
inline NodeId bottom_bot(NodeId self, int slot) {
  if (!is_player_node(self) || slot < 0 || slot >= HINT_BUTTONS) return NodeId::None;
  return nodeLayouts[static_cast<int>(self)].neighbors[HINT_FIRST_BUTTON + slot];
}

// プレイヤーがそのBotにつながっているか（プレイヤーの下部のボタンにそのBotがある）。
inline bool bot_links(NodeId bot, NodeId player) {
  if (!is_bot_node(bot) || !is_player_node(player)) return false;
  for (NodeId neighbor : nodeLayouts[static_cast<int>(player)].neighbors)
    if (neighbor == bot) return true;
  return false;
}

// 宛先が両隣（左端・右端のボタンのノード）か。両隣宛ては両端のボタンで直接送れるので、帯を光らせない。
inline bool end_neighbor(NodeId self, NodeId node) {
  if (!is_player_node(self) || node == NodeId::None) return false;
  const NodeLayout& layout = nodeLayouts[static_cast<int>(self)];
  return layout.neighbors[0] == node || layout.neighbors[3] == node;
}

// Botのボタンの帯で、nodeの枠の見え方。destinationは先頭パケットの宛先で、FIFOが空ならNone。
inline HintCell hint_cell(NodeId self, NodeId bot, NodeId node, NodeId destination) {
  if (node == self || !bot_links(bot, node)) return HintCell::Unlinked;
  if (node == destination && !end_neighbor(self, destination)) return HintCell::Lit;
  return HintCell::Linked;
}

// 先頭パケットの宛先から、2つのBotのボタンの帯を求める。FIFOが空ならdestinationにNoneを渡し、どの枠も光らない。
inline TutorialHint tutorial_hint(NodeId self, NodeId destination) {
  TutorialHint hint;
  for (int slot = 0; slot < HINT_BUTTONS; ++slot) {
    const NodeId bot = bottom_bot(self, slot);
    for (int cell = 0; cell < HINT_CELLS; ++cell)
      hint.bands[slot].cells[cell] = hint_cell(self, bot, hint_cell_node(cell), destination);
  }
  return hint;
}

// RGB565の各成分をpercent%の明るさにする。
constexpr uint16_t scale565(uint16_t color, int percent) {
  return static_cast<uint16_t>(((color >> 11) * percent / 100) << 11 | (((color >> 5) & 0x3f) * percent / 100) << 5 |
                               (color & 0x1f) * percent / 100);
}
// つながる枠の塗り。ノードの色を暗くする。
constexpr uint16_t hint_linked_color(uint16_t nodeColor) { return scale565(nodeColor, HINT_LINKED_PERCENT); }
// つながらない枠の線と文字。ボタンの地の色より暗い色で、地が黒（押下中）なら暗い灰色。
constexpr uint16_t hint_faint_color(uint16_t background) {
  return background == 0 ? HINT_FAINT_ON_BLACK : scale565(background, HINT_FAINT_PERCENT);
}

// Botの混雑。自ノードの下部のslot番目のBotの使用量（0〜4）。botsが来ていない間・Botが決まらない間は-1で、何も出さない。
inline int bot_load(const PlayerStatus& status, NodeId self, int slot) {
  const NodeId bot = bottom_bot(self, slot);
  if (!status.botsKnown || !is_bot_node(bot)) return -1;
  return status.bots[static_cast<int>(bot) - static_cast<int>(NodeId::B1)];
}
// シグナルタワーと同じ色の対応。0〜1が青、2が緑、3が黄、4が赤。
constexpr uint16_t bot_load_color(int count) {
  return count <= 1 ? BOT_LOAD_BLUE : count == 2 ? BOT_LOAD_GREEN : count == 3 ? BOT_LOAD_YELLOW : BOT_LOAD_RED;
}
// FULLを出すか。満杯（4件）のBotだけ。
constexpr bool bot_load_full(int count) { return count >= BOT_LOAD_MAX; }

#endif // TUTORIAL_HINT_HPP_
