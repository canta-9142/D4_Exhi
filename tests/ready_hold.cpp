#include <cassert>
#include <cstdio>
#include "../src/player/ready_hold.hpp"

int main() {
  ReadyHold hold;
  assert(!hold.update(9, true, 0));
  assert(!hold.update(9, true, 3000)); // 設定・復旧時の押しっぱなしは引き継がない。
  assert(!hold.update(1, true, 3001));
  assert(!hold.update(9, true, 6001)); // 片方だけの解放では再開しない。
  assert(!hold.update(0, true, 6002));
  assert(!hold.update(1, true, 6003));
  assert(!hold.update(9, true, 6200)); // 後から押した端から計測。
  assert(!hold.update(15, true, 8199) && hold.progress_ms() == 1999);
  assert(hold.update(9, true, 8200)); // 内側の2ボタンは無関係。
  assert(!hold.update(9, true, 12000));
  assert(!hold.update(1, true, 12001));
  assert(!hold.update(9, true, 15000));
  hold.update(0, true, 15001);
  hold.update(9, true, 15002);
  hold.update(9, true, 16002);
  assert(hold.progress_ms() == 1000);
  hold.update(8, true, 16003);
  assert(hold.progress_ms() == 0);
  hold.update(9, true, 16004);
  assert(!hold.update(9, true, 18003));
  assert(hold.update(9, true, 18004));
  hold.update(0, true, UINT32_MAX - 1500);
  hold.update(9, true, UINT32_MAX - 1000);
  assert(!hold.update(9, true, 998));
  assert(hold.update(9, true, 999)); // millisの周回。
  hold.update(0, true, 1000);
  hold.update(9, true, 1001);
  hold.update(9, false, 2001);
  assert(hold.progress_ms() == 0 && !hold.update(9, true, 5000));
  std::puts("PASS: two-second hold, separate presses, inner buttons, cancellation, release latch, clock wrap");
}
