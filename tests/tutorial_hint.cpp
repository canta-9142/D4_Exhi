#include <cassert>
#include <cstdio>
#include <initializer_list>
#include "../src/player/tutorial_hint.hpp"

using H = HintCell;

static NodeId player(char name) { return static_cast<NodeId>(static_cast<int>(NodeId::P1) + (name - 'A')); }
static NodeId bot(char name) { return static_cast<NodeId>(static_cast<int>(NodeId::B1) + (name - 'I')); }

static void expect_band(const HintBand& band, std::initializer_list<H> cells) {
  int i = 0;
  for (H cell : cells) assert(band.cells[i++] == cell);
  assert(i == HINT_CELLS);
}

struct Rect {
  int x, y, w, h;
};
static bool overlaps(const Rect& a, const Rect& b) {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}
static bool inside(const Rect& inner, const Rect& outer) {
  return inner.x >= outer.x && inner.y >= outer.y && inner.x + inner.w <= outer.x + outer.w &&
         inner.y + inner.h <= outer.y + outer.h;
}

// 仕様の「ノードIDと表示」のボタン配置表（左から2番目・3番目）。
static void test_bottom_bots() {
  const char table[8][3] = {"IK", "JI", "JK", "JI", "KI", "IJ", "IK", "JK"};
  for (int i = 0; i < 8; ++i) {
    const NodeId self = hint_cell_node(i);
    assert(bottom_bot(self, 0) == bot(table[i][0]) && bottom_bot(self, 1) == bot(table[i][1]));
    assert(bottom_bot(self, -1) == NodeId::None && bottom_bot(self, 2) == NodeId::None);
  }
  assert(bottom_bot(NodeId::None, 0) == NodeId::None && bottom_bot(NodeId::B1, 0) == NodeId::None);
  // Botにつながるプレイヤー。配置表から数えた結果と一致する。
  const char* linked[3] = {"ABDEFG", "BCDFH", "ACEGH"};
  for (int b = 0; b < 3; ++b) {
    for (int p = 0; p < 8; ++p) {
      bool expected = false;
      for (const char* c = linked[b]; *c; ++c) expected |= *c - 'A' == p;
      assert(bot_links(static_cast<NodeId>(static_cast<int>(NodeId::B1) + b), hint_cell_node(p)) == expected);
    }
  }
  assert(!bot_links(NodeId::P1, NodeId::P2) && !bot_links(NodeId::B1, NodeId::B2) && !bot_links(NodeId::B1, NodeId::None));
}

// 参考画像の3例。自ノードはAで、下部のBotはI（左から2番目）とK（左から3番目）。
static void test_mock_examples() {
  const NodeId a = player('A');
  // 宛先D: Iの帯だけでDが光り、KはDにつながらないので光らない。
  TutorialHint hint = tutorial_hint(a, player('D'));
  expect_band(hint.bands[0], {H::Unlinked, H::Linked, H::Unlinked, H::Lit, H::Linked, H::Linked, H::Linked, H::Unlinked});
  expect_band(hint.bands[1], {H::Unlinked, H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Linked});
  // 宛先G: IとKの両方でGが光る。右端のボタンのHもGにつながるが、両端のボタンには帯を出さない。
  hint = tutorial_hint(a, player('G'));
  expect_band(hint.bands[0], {H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Linked, H::Linked, H::Lit, H::Unlinked});
  expect_band(hint.bands[1], {H::Unlinked, H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Unlinked, H::Lit, H::Linked});
  // 宛先B（隣接プレイヤー）: どの枠も光らない。
  hint = tutorial_hint(a, player('B'));
  expect_band(hint.bands[0], {H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Linked, H::Linked, H::Linked, H::Unlinked});
  expect_band(hint.bands[1], {H::Unlinked, H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Unlinked, H::Linked, H::Linked});
  // FIFOが空（宛先なし）でも、つながる枠は同じ見え方で、光らない。
  assert(tutorial_hint(a, NodeId::None) == hint);
  assert(tutorial_hint(a, player('H')) == hint); // 右隣のHも光らない。
  assert(tutorial_hint(a, player('D')) != hint);
}

static int lit_count(const TutorialHint& hint) {
  int count = 0;
  for (const HintBand& band : hint.bands)
    for (H cell : band.cells) count += cell == H::Lit;
  return count;
}

// 8ノードすべての宛先。両隣以外なら2つのBotの少なくとも一方の枠が光り、両隣なら光らない。自分の枠は常につながらない。
static void test_all_destinations() {
  for (int s = 0; s < 8; ++s) {
    const NodeId self = hint_cell_node(s);
    const NodeLayout& layout = nodeLayouts[static_cast<int>(self)];
    int far = 0;
    for (int d = -1; d < 8; ++d) {
      const NodeId destination = d < 0 ? NodeId::None : hint_cell_node(d);
      const TutorialHint hint = tutorial_hint(self, destination);
      for (const HintBand& band : hint.bands) assert(band.cells[s] == H::Unlinked);
      if (d < 0 || d == s) {
        assert(lit_count(hint) == 0);
        continue;
      }
      const bool neighbor = destination == layout.neighbors[0] || destination == layout.neighbors[3];
      assert(end_neighbor(self, destination) == neighbor);
      if (neighbor) {
        assert(lit_count(hint) == 0);
        continue;
      }
      ++far;
      bool lit = false;
      for (int slot = 0; slot < HINT_BUTTONS; ++slot) {
        for (int cell = 0; cell < HINT_CELLS; ++cell) {
          const H state = hint.bands[slot].cells[cell];
          // 光るのは宛先の枠だけで、そのBotにつながる場合に限る。
          if (cell != d) assert(state != H::Lit);
          else assert((state == H::Lit) == bot_links(bottom_bot(self, slot), destination));
          // つながる・つながらないは宛先によらず、隣接関係だけで決まる。
          if (cell != s && cell != d) assert((state == H::Linked) == bot_links(bottom_bot(self, slot), hint_cell_node(cell)));
        }
        lit |= hint.bands[slot].cells[d] == H::Lit;
      }
      assert(lit);
    }
    assert(far == 5); // 自分と両隣を除く5ノード。
  }
  // Botが宛先（通常は来ない）や、自ノードが未定のときは何も光らない。
  assert(lit_count(tutorial_hint(NodeId::P1, NodeId::B1)) == 0);
  assert(tutorial_hint(NodeId::None, NodeId::P2) == TutorialHint{});
}

// Botの混雑。シグナルタワーと同じ色、4件だけFULL、未受信なら出さない。
static void test_bot_load() {
  const uint16_t colors[5] = {BOT_LOAD_BLUE, BOT_LOAD_BLUE, BOT_LOAD_GREEN, BOT_LOAD_YELLOW, BOT_LOAD_RED};
  for (int count = 0; count <= 4; ++count) {
    assert(bot_load_color(count) == colors[count]);
    assert(bot_load_full(count) == (count == 4));
  }
  PlayerStatus status{};
  status.bots[0] = 1; // B1（I）
  status.bots[1] = 2; // B2（J）
  status.bots[2] = 4; // B3（K）
  for (int s = 0; s < 8; ++s)
    for (int slot = 0; slot < HINT_BUTTONS; ++slot) assert(bot_load(status, hint_cell_node(s), slot) == -1);
  status.botsKnown = true;
  const char table[8][3] = {"IK", "JI", "JK", "JI", "KI", "IJ", "IK", "JK"};
  const int load[3] = {1, 2, 4};
  for (int s = 0; s < 8; ++s)
    for (int slot = 0; slot < HINT_BUTTONS; ++slot)
      assert(bot_load(status, hint_cell_node(s), slot) == load[table[s][slot] - 'I']);
  assert(bot_load(status, NodeId::None, 0) == -1 && bot_load(status, NodeId::B2, 1) == -1);
  // 自ノードA（I・K）: Iは青、Kは満杯で赤とFULL。
  assert(bot_load_color(bot_load(status, NodeId::P1, 0)) == BOT_LOAD_BLUE && !bot_load_full(bot_load(status, NodeId::P1, 0)));
  assert(bot_load_color(bot_load(status, NodeId::P1, 1)) == BOT_LOAD_RED && bot_load_full(bot_load(status, NodeId::P1, 1)));
}

// 帯・バー・FULLの札がボタン枠に収まり、ボタンの文字と互いに重ならない。
static void test_layout() {
  for (int button = HINT_FIRST_BUTTON; button < HINT_FIRST_BUTTON + HINT_BUTTONS; ++button) {
    const Rect frame{button_frame_x(button), BUTTON_FRAME_Y, BUTTON_FRAME_WIDTH, BUTTON_FRAME_HEIGHT};
    const Rect letter{button_letter_x(button), BUTTON_LETTER_Y, BUTTON_LETTER_WIDTH, BUTTON_LETTER_HEIGHT};
    const Rect band{hint_band_x(button), HINT_BAND_Y, HINT_BAND_WIDTH, HINT_CELL_HEIGHT};
    const Rect bar{button_frame_x(button), BOT_BAR_Y, BUTTON_FRAME_WIDTH, BOT_BAR_HEIGHT};
    const Rect full{bot_full_x(button), BOT_FULL_Y, BOT_FULL_WIDTH, BOT_FULL_HEIGHT};
    const Rect fullText{bot_full_x(button) + (BOT_FULL_WIDTH - 23 * BOT_FULL_TEXT_SIZE) / 2,
                        BOT_FULL_Y + (BOT_FULL_HEIGHT - 7 * BOT_FULL_TEXT_SIZE) / 2, 23 * BOT_FULL_TEXT_SIZE, 7 * BOT_FULL_TEXT_SIZE};
    assert(inside(letter, frame) && inside(band, frame) && inside(bar, frame) && inside(full, frame) && inside(fullText, full));
    assert(!overlaps(band, letter) && !overlaps(bar, letter) && !overlaps(full, letter));
    assert(!overlaps(band, bar) && !overlaps(band, full) && !overlaps(bar, full));
    assert(band.y + band.h < letter.y); // 帯と文字の間に1行以上空ける。
    for (int cell = 0; cell < HINT_CELLS; ++cell) {
      const Rect box{hint_cell_x(button, cell), HINT_BAND_Y, HINT_CELL_WIDTH, HINT_CELL_HEIGHT};
      assert(inside(box, band));
      if (cell) assert(!overlaps(box, {hint_cell_x(button, cell - 1), HINT_BAND_Y, HINT_CELL_WIDTH, HINT_CELL_HEIGHT}));
      // 枠の中の名前（5×7）は、光る枠の太枠の内側に収まる。
      const Rect text{box.x + HINT_TEXT_DX, box.y + HINT_TEXT_DY, 5, 7};
      const Rect inner{box.x + HINT_LIT_BORDER, box.y + HINT_LIT_BORDER, box.w - 2 * HINT_LIT_BORDER, box.h - 2 * HINT_LIT_BORDER};
      assert(inside(text, inner));
    }
  }
  // 帯は2番目・3番目のボタンだけで、両端のボタンには掛からない。
  assert(hint_band_x(HINT_FIRST_BUTTON) > button_frame_x(0) + BUTTON_FRAME_WIDTH);
  assert(hint_band_x(HINT_FIRST_BUTTON + HINT_BUTTONS - 1) + HINT_BAND_WIDTH <= button_frame_x(3));
  // つながる枠の塗りは元の色より暗く、つながらない枠の線は地の色と区別できる。
  for (uint16_t color : {0xF800, 0xFDA0, 0xFFE0, 0x07E0, 0x07FF, 0x001F, 0x780F, 0xFE19}) {
    const uint16_t dim = hint_linked_color(color);
    assert(dim != color && (dim >> 11) <= (color >> 11) && ((dim >> 5) & 0x3f) <= ((color >> 5) & 0x3f) && (dim & 0x1f) <= (color & 0x1f));
  }
  for (uint16_t background : {0x0000, 0xFFFF, 0x8410, 0x9A60}) assert(hint_faint_color(background) != background);
}

int main() {
  test_bottom_bots();
  test_mock_examples();
  test_all_destinations();
  test_bot_load();
  test_layout();
  std::puts("PASS: hint bands for every node and destination, the three mock examples, bot load colors and FULL, bottom bots for every node, band and bar layout clear of the button letters");
}
