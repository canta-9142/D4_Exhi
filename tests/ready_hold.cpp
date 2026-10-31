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

  // 待機・結果・練習の段階の切替（enabledが一度でもfalseになる）では、押したままを次の段階へ持ち越さない。
  ReadyHold stage;
  stage.update(0, true, 0);
  stage.update(9, true, 10);
  assert(!stage.update(9, true, 1500) && stage.progress_ms() == 1490);
  assert(!stage.update(9, false, 1501) && stage.progress_ms() == 0); // 段階が変わった。
  assert(!stage.update(9, true, 1502)); // 押したままでは計測を再開しない。
  assert(!stage.update(9, true, 9000) && stage.progress_ms() == 0);
  assert(!stage.update(0, true, 9001));
  assert(!stage.update(9, true, 9002) && stage.progress_ms() == 0); // 離した後の押し直しから計測する。
  assert(!stage.update(9, true, 11001) && stage.progress_ms() == 1999);
  assert(stage.update(9, true, 11002) && stage.progress_ms() == 0);
  // 成立後は両端を離すまで再通知しない（確認待ちの間に押し続けても、要求は1回だけ）。
  assert(!stage.update(9, true, 20000) && !stage.update(9, true, 40000));
  // 長押しの途中で内側のボタンを押し離ししても、計測は続く。
  stage.update(0, true, 40001);
  stage.update(9, true, 40002);
  assert(!stage.update(15, true, 40500) && !stage.update(9, true, 41000) && !stage.update(11, true, 41500));
  assert(stage.update(13, true, 42002));
  // 計測の途中で端を離したら、押し直した時点から2秒数え直す。
  stage.update(0, true, 42003);
  stage.update(9, true, 42004);
  assert(!stage.update(1, true, 43500) && stage.progress_ms() == 0);
  assert(!stage.update(9, true, 43600) && !stage.update(9, true, 45599) && stage.update(9, true, 45600));
  std::puts("PASS: two-second hold, separate presses, inner buttons, cancellation, release latch, stage change, clock wrap");
}
