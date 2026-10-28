#include <algorithm>
#include <cassert>
#include <cstdint>
#include <cstdio>
#include <random>
#include <vector>
#include "../src/player/frame_damage.hpp"

namespace {
constexpr int WIDTH = FrameDamage::WIDTH;
constexpr int HEIGHT = FrameDamage::HEIGHT;
using Pixels = std::vector<uint16_t>;

unsigned verify(const Pixels& previous, const Pixels& next, bool force = false) {
  FrameDamage damage;
  damage.compare(previous.data(), next.data());
  if (force) damage.full();
  Pixels lcd = previous;
  std::vector<bool> covered(WIDTH * HEIGHT);
  int previousX = -1, previousBottom = 0;
  unsigned pixels = 0;
  damage.forEachSpan([&](int x, int y, int w, int h) {
    assert(w == 16 && h > 0 && h % 16 == 0);
    assert(x >= 0 && y >= 0 && x + w <= WIDTH && y + h <= HEIGHT);
    assert(x >= previousX && (x != previousX || y > previousBottom));
    previousX = x;
    previousBottom = y + h;
    for (int row = y; row < y + h; ++row) {
      for (int col = x; col < x + w; ++col) {
        const int index = row * WIDTH + col;
        assert(!covered[index]);
        covered[index] = true;
        lcd[index] = next[index];
        ++pixels;
      }
    }
  });
  assert(pixels == damage.changedTiles() * 16 * 16);
  assert(lcd == next); // LCDへ矩形を適用した結果が全画面描画と一致する。
  for (int y = 0; y < HEIGHT; ++y)
    for (int x = 0; x < WIDTH; ++x)
      assert(covered[y * WIDTH + x] == damage.containsPixel(x, y));
  return damage.changedTiles();
}
}

int main() {
  FrameDamage marked;
  marked.markRect(-4, -4, 5, 5);
  marked.markRect(479, 319, 100, 100);
  marked.markRect(480, 320, 20, 20);
  marked.markRect(8, 8, 0, 100);
  assert(marked.changedTiles() == 2 && marked.containsPixel(0, 0) && marked.containsPixel(479, 319));
  marked.markRect(0, 0, 16, 16);
  assert(marked.changedTiles() == 2);
  marked.markRect(0, 0, WIDTH, HEIGHT);
  assert(marked.changedTiles() == 600);
  Pixels old(WIDTH * HEIGHT, 0), next = old;
  assert(verify(old, next) == 0);
  assert(verify(old, next, true) == 600); // 初回は同一画像でも全画面を送る。
  for (const int y : {0, 15, 16, 159, 160, 319}) {
    for (const int x : {0, 15, 16, 239, 240, 479}) {
      next = old;
      next[y * WIDTH + x] = 1;
      assert(verify(old, next) == 1);
    }
  }
  next = old;
  for (int y = 0; y < HEIGHT; y += 16)
    for (int x = 0; x < WIDTH; x += 16)
      next[y * WIDTH + x] = 1;
  assert(verify(old, next) == 600); // 各タイルの1画素だけが変わる場合。
  next = old;
  for (int y = 0; y < HEIGHT; y += 16)
    for (int x = 0; x < WIDTH; x += 16)
      if ((x / 16 + y / 16) % 2) next[y * WIDTH + x] = 1;
  assert(verify(old, next) == 300); // 縦に結合できない場合。
  std::fill(next.begin(), next.end(), 0xFFFF);
  assert(verify(old, next) == 600);
  old = next;
  std::fill(next.begin(), next.end(), 0);
  assert(verify(old, next) == 600); // 消去も変更として扱う。
  std::mt19937 random(20261026);
  for (unsigned frame = 0; frame < 100; ++frame) {
    old = next;
    for (unsigned i = 0; i < frame * 13; ++i) next[random() % next.size()] ^= uint16_t(random());
    verify(old, next);
    assert(verify(next, next) == 0); // 前回の差分が残らない。
  }
  std::puts("PASS: full-screen diff, tile boundaries, spans, forced refresh, erasure, 100 random frames");
}
