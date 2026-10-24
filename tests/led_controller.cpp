#include <cassert>
#include <iostream>
#include <limits>
#include <string>
#include "../src/led/controller.hpp"

struct TestPort {
  bool connected = true;
  std::string input;
  std::string output;
  size_t position = 0;
  int writable = 256;

  explicit operator bool() const { return connected; }
  int available() const { return input.size() - position; }
  int read() { return available() ? static_cast<unsigned char>(input[position++]) : -1; }
  int availableForWrite() const { return writable; }
  size_t write(const uint8_t* bytes, size_t count) {
    output.append(reinterpret_cast<const char*>(bytes), count);
    return count;
  }
};

void expect(led::Color color, unsigned r, unsigned g, unsigned b) {
  assert(color.r == r && color.g == g && color.b == b);
}

int main() {
  led::Controller controller;
  TestPort port;
  uint32_t now = 0;
  auto poll = [&] { return controller.poll(port, "001122334455", now); };
  auto receive = [&](const std::string& bytes) {
    port.input = bytes;
    port.position = 0;
    bool configured = false;
    while (port.available()) configured |= poll();
    return configured;
  };
  auto pixel = [&](size_t strip = 1) { return controller.pixel(strip, 0, now); };

  // HELLOの再試行と送信バッファ不足。
  port.writable = 0;
  poll();
  assert(port.output.empty());
  port.writable = 256;
  poll();
  assert(port.output == "HELLO 001122334455\n");
  port.output.clear();
  now = 999;
  poll();
  assert(port.output.empty());
  now = 1000;
  poll();
  assert(port.output == "HELLO 001122334455\n");
  assert(!controller.ready());
  receive("LIGHT P1 P1 outgoing\nBUFFER 4\nCONFIG P2\n");
  assert(!controller.ready());

  // 全制御ノードのGPIOと仮のLED数。
  const char* nodes[] = {"B1", "B2", "B3", "P1", "P3", "P5", "P7"};
  const std::string pins[] = {"0134567", "013456", "012345", "01", "01", "01", "01"};
  for (size_t n = 0; n < 7; ++n) {
    assert(receive(std::string("CONFIG ") + nodes[n] + "\n"));
    assert(controller.strip_count() == pins[n].size());
    for (size_t i = 0; i < controller.strip_count(); ++i) {
      assert(controller.route(i).pin == pins[n][i] - '0');
      assert(controller.route(i).count == 1);
    }
  }

  // 分割行とCRLF。CONFIGまでは適用しない。
  receive("CONFIG B");
  assert(controller.strip_count() == 2);
  assert(receive("1\r\n"));
  assert(controller.strip_count() == 7);
  expect(pixel(0), 0, 0, 0);
  port.output.clear();
  now += 2000;
  poll();
  assert(port.output.empty());

  // BUFFERは0〜4件。経路と独立して保持する。
  const led::Color tower[] = {{0, 0, 255}, {0, 0, 255}, {0, 255, 0},
                            {255, 255, 0}, {255, 0, 0}};
  for (unsigned used = 0; used <= 4; ++used) {
    receive("BUFFER " + std::to_string(used) + "\n");
    expect(pixel(0), tower[used].r, tower[used].g, tower[used].b);
  }
  receive("BUFFER 5\nBUFFER -1\nBUFFER 2x\nBUFFER 00\nBUFFER\nBUFFER 1 extra\n");
  expect(pixel(0), 255, 0, 0);

  // 同方向・逆方向の加算、RGB各成分の飽和、10msごとの減衰。
  receive("LIGHT P1 P1 outgoing\nLIGHT P1 P1 outgoing\nLIGHT P1 P6 incoming\n");
  expect(pixel(), 255, 0, 255);
  expect(pixel(2), 0, 0, 0);
  now += 10;
  expect(pixel(), 255, 0, 128);
  now += 10;
  expect(pixel(), 128, 0, 64);
  expect(pixel(0), 255, 0, 0);
  now += 100;
  poll();
  expect(pixel(), 0, 0, 0);

  // 未接続経路、タワーへのLIGHT、不正な目的地・方向・引数は無視。
  receive("LIGHT P3 P1 outgoing\nLIGHT B1 P1 outgoing\nLIGHT P1 P0 outgoing\n"
          "LIGHT P1 P9 outgoing\nLIGHT P1 B4 outgoing\nLIGHT P1 P1 sideways\n"
          "LIGHT P1 P1 outgoing extra\nLIGHT P1 P1\nLIGHT P1 P01 outgoing\n");
  expect(pixel(), 0, 0, 0);
  expect(pixel(0), 255, 0, 0);
  receive("LIGHT P1 B1 outgoing\n");
  expect(pixel(), 255, 255, 255);
  receive("CLEAR extra\n");
  expect(pixel(), 255, 255, 255);
  receive("CLEAR\n");
  assert(controller.ready());
  expect(pixel(), 0, 0, 0);
  expect(pixel(0), 0, 0, 0);

  // 長過ぎる行、NULを含む行、タイムアウト後の行の後半を実行しない。
  receive(std::string(led::Controller::MAX_LINE_BYTES + 1, ' ') + "BUFFER 4\n");
  expect(pixel(0), 0, 0, 0);
  receive(std::string("BUFFER 4\0ignored\n", 17));
  expect(pixel(0), 0, 0, 0);
  receive("BUFF");
  now += led::Controller::RECEIVE_TIMEOUT_MS;
  poll();
  receive("ER 4\n");
  expect(pixel(0), 0, 0, 0);
  receive("BUFFER 3\n");
  expect(pixel(0), 255, 255, 0);
  receive("CONFIG B9\nCONFIG B2 extra\nUNKNOWN\n");
  assert(controller.strip_count() == 7);
  expect(pixel(0), 255, 255, 0);

  // 再CONFIGでは残像とタワーを初期化。プレイヤー制御ノードのBUFFERは無視。
  receive("LIGHT P1 P1 outgoing\nCONFIG B1\n");
  expect(pixel(), 0, 0, 0);
  expect(pixel(0), 0, 0, 0);
  receive("CONFIG P1\nBUFFER 4\nLIGHT P8 P4 outgoing\n");
  expect(pixel(0), 0, 0, 0);
  expect(pixel(1), 0, 255, 0);

  // 同時点灯数が上限を超えても既存演出を保持し、終了後は空きを再利用。
  receive("CLEAR\n");
  for (size_t i = 0; i < led::Controller::MAX_LIGHTS; ++i) receive("LIGHT P2 P1 outgoing\n");
  receive("LIGHT P8 P6 outgoing\n");
  expect(pixel(0), 255, 0, 0);
  expect(pixel(1), 0, 0, 0);
  now += 110;
  receive("LIGHT P8 P6 outgoing\n");
  expect(pixel(0), 0, 0, 0);
  expect(pixel(1), 0, 0, 255);

  // 受信が大量にあっても1回のpollで1024バイトまでに制限。
  port.input = std::string(2048, '\n');
  port.position = 0;
  poll();
  assert(port.available() == 1024);
  poll();
  assert(port.available() == 0);

  // 短いCDC未接続判定は許容し、100ms継続したら消灯・再識別。
  port.connected = false;
  assert(!poll());
  now += 99;
  assert(!poll());
  assert(controller.ready());
  port.connected = true;
  poll();
  assert(controller.ready());
  port.connected = false;
  poll();
  now += 100;
  assert(poll());
  assert(!controller.ready() && controller.strip_count() == 0);
  port.connected = true;
  port.output.clear();
  poll();
  assert(port.output == "HELLO 001122334455\n");

  // millis()の周回を跨いでもアニメーションの経過時間が正しい。
  now = std::numeric_limits<uint32_t>::max() - 4;
  receive("CONFIG P1\nLIGHT P2 P1 outgoing\n");
  now += 10;
  poll();
  expect(pixel(0), 128, 0, 0);

  // 将来LED数を増やした際の移動・尾・逆方向も実際の描画関数で確認。
  const led::Color red{255, 0, 0};
  expect(led::pulse_pixel(red, false, 4, 0, 0), 255, 0, 0);
  expect(led::pulse_pixel(red, false, 4, 1, 0), 0, 0, 0);
  expect(led::pulse_pixel(red, false, 4, 0, 10), 128, 0, 0);
  expect(led::pulse_pixel(red, false, 4, 1, 10), 255, 0, 0);
  expect(led::pulse_pixel(red, false, 4, 3, 40), 128, 0, 0);
  expect(led::pulse_pixel(red, true, 4, 3, 0), 255, 0, 0);
  expect(led::pulse_pixel(red, true, 4, 2, 10), 255, 0, 0);
  expect(led::pulse_pixel(red, true, 4, 3, 10), 128, 0, 0);
  expect(led::pulse_pixel(red, false, 4, 3, 120), 0, 0, 0);
  expect(led::pulse_pixel(red, true, 0, 0, 0), 0, 0, 0);
  std::cout << "LED controller checks passed\n";
}
