#include <iostream>
#include "../src/led/firmware.hpp"

void expect_dark() {
  for (const auto& pixels : retainedPixels) {
    assert(std::all_of(pixels.begin(), pixels.end(), [](uint8_t c) { return c == 0; }));
  }
}

void expect_greeting_first() {
  expect_dark();
  assert(!controller.ready() && outputCount == 0 && Serial.position == 0);
}

void expect_tower(uint8_t r, uint8_t g, uint8_t b) {
  assert(retainedPixels[0].size() == 8 * 3);
  for (size_t i = 0; i < 8; ++i) {
    assert(retainedPixels[0][i * 3] == r);
    assert(retainedPixels[0][i * 3 + 1] == g);
    assert(retainedPixels[0][i * 3 + 2] == b);
  }
}

int main() {
  const std::string hello = "HELLO 001122334455\n";
  const std::string identify = R"({"type":"identify","data":{}})" "\n";
  auto receive = [&](const std::string& input) {
    Serial.input = input;
    Serial.position = 0;
    loop();
  };
  for (auto& pixels : retainedPixels) pixels.assign(3, 255);
  // 出力の初期化に一度失敗しても、全出力を消灯してから通信を開始する。
  failNextBegin = true;
  setup();
  assert(testMillis == 10 && showsBeforeSerial == retainedPixels.size());
  for (const auto& route : led::ROUTES) {
    const auto& pixels = retainedPixels.at(route.pin);
    assert(pixels.size() >= route.count * 3u);
    assert(std::all_of(pixels.begin(), pixels.end(), [](uint8_t c) { return c == 0; }));
  }
  assert(!controller.ready() && outputCount == 0 && Serial.output.empty());
  loop();
  assert(Serial.output == hello);
  // 最初のCONFIGでメモリ確保に失敗しても、未初期化の出力を操作しない。
  Serial.output.clear();
  Serial.beforeWrite = expect_dark;
  failNextAllocation = true;
  receive("CONFIG B1 false\n");
  assert(!controller.ready() && outputCount == 0 && Serial.output == hello);
  Serial.beforeWrite = nullptr;
  // 起動時の一時出力を解放した後も、通常の設定と描画ができる。
  Serial.output.clear();
  receive("CONFIG B1 false\nBUFFER 4\n");
  assert(controller.ready() && outputCount == 7 && Serial.output.empty());
  expect_tower(255, 0, 0);

  // 点灯中のidentify・不正CONFIG・受信異常は、全出力の消灯後にHELLOを返す。
  const std::string invalid[] = {
    identify, "CONFIG B9 false\n", "UNKNOWN\n", std::string(97, 'x') + "\n",
    std::string(1, '\x01') + "\n"
  };
  for (const auto& input : invalid) {
    receive("CONFIG B1 false\nBUFFER 4\nLIGHT P1 P1 outgoing\n");
    assert(retainedPixels[0][0] == 255);
    Serial.output.clear();
    Serial.beforeWrite = expect_dark;
    receive(input);
    assert(!controller.ready() && outputCount == 0 && Serial.output == hello);
    expect_dark();
    Serial.beforeWrite = nullptr;
  }

  // 部分行タイムアウトと切断も、同じ消灯処理を通る。
  receive("CONFIG B1 false\nBUFFER 4\nCONF");
  Serial.output.clear();
  Serial.beforeWrite = expect_dark;
  testMillis += 1000;
  loop();
  assert(!controller.ready() && outputCount == 0 && Serial.output == hello);
  receive("\n");
  Serial.beforeWrite = nullptr;
  receive("CONFIG B1 false\nBUFFER 4\n");
  Serial.output.clear();
  Serial.connected = false;
  loop();
  testMillis += 100;
  loop();
  expect_dark();
  assert(!controller.ready() && outputCount == 0 && Serial.output.empty());
  Serial.connected = true;
  Serial.beforeWrite = expect_dark;
  loop();
  assert(Serial.output == hello);
  Serial.beforeWrite = nullptr;

  // 保留中の2応答は、出力初期化の失敗と後続CONFIGを挟んでも失わない。
  Serial.output.clear();
  Serial.writable = 0;
  failNextBegin = true;
  receive(identify + identify + "CONFIG B1 false\nCONFIG P1 false\n");
  assert(controller.ready() && outputCount == 2 && Serial.output.empty());
  Serial.writable = 256;
  loop();
  assert(Serial.output == hello + hello);
  Serial.output.clear();
  testMillis += 1000;
  loop();
  assert(Serial.output.empty());

  // 応答待ちがなくても、初期化失敗時のHELLOを後続CONFIGで失わない。
  Serial.writable = 0;
  failNextAllocation = true;
  receive("CONFIG B1 false\nCONFIG P1 false\n");
  assert(controller.ready() && Serial.output.empty());
  Serial.writable = 256;
  loop();
  assert(Serial.output == hello);

  // 100ms未満の切断では、部分行とCDC内の続きの入力を保持する。
  Serial.output.clear();
  receive("CONFIG B1 false\nBUFFER 4\nBUFF");
  Serial.input = "ER 2\n";
  Serial.position = 0;
  Serial.connected = false;
  loop();
  testMillis += 98;
  loop();
  assert(controller.ready() && Serial.position == 0 && retainedPixels[0][0] == 255);
  Serial.connected = true;
  loop();
  assert(controller.ready() && Serial.output.empty());
  testMillis += led::FRAME_MS;
  loop();
  expect_tower(0, 255, 0);

  // 切断確定では、CDC内の旧CONFIG・identify・部分行を全て捨てる。
  Serial.input = std::string(2048, '\n') + "CONFIG B2 true\n" + identify + "CONF";
  Serial.position = 0;
  Serial.connected = false;
  loop();
  testMillis += 100;
  loop();
  assert(Serial.available() == 0 && Serial.output.empty());
  assert(!controller.ready() && outputCount == 0);
  expect_dark();
  // 切断確定後にキューへ残った入力も、切断中の次のpollで破棄する。
  Serial.input = "CONFIG B2 true\n";
  Serial.position = 0;
  loop();
  assert(Serial.available() == 0 && !controller.ready());
  Serial.input.clear();
  Serial.position = 0;
  Serial.connected = true;
  Serial.beforeWrite = expect_greeting_first;
  loop();
  assert(Serial.output == hello && !controller.ready() && outputCount == 0);
  Serial.beforeWrite = nullptr;
  Serial.output.clear();
  receive("CONFIG B1 false\nBUFFER 4\n");

  // 再接続時の新CONFIGは、送信バッファ不足でHELLOを送れない間は保留。
  Serial.connected = false;
  loop();
  testMillis += 100;
  loop();
  Serial.connected = true;
  Serial.writable = 0;
  Serial.input = "CONFIG P1 false\n";
  Serial.position = 0;
  loop();
  assert(!controller.ready() && Serial.position == 0 && Serial.output.empty());
  Serial.writable = 256;
  Serial.beforeWrite = expect_greeting_first;
  loop();
  assert(controller.ready() && outputCount == 2 && Serial.output == hello);
  assert(Serial.available() == 0);
  Serial.beforeWrite = nullptr;
  std::cout << "LED firmware startup and recovery checks passed\n";
}
