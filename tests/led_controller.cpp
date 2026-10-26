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
  // 起動直後も、HELLO送信前にキュー内のCONFIGを適用しない。
  {
    led::Controller fresh;
    TestPort queued;
    queued.input = "CONFIG B2 true\n";
    queued.writable = 0;
    fresh.poll(queued, "001122334455", 0);
    assert(!fresh.ready() && queued.position == 0 && queued.output.empty());
    queued.writable = 256;
    fresh.poll(queued, "001122334455", 1);
    assert(fresh.ready() && queued.output == "HELLO 001122334455\n");
  }
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
  const std::string hello = "HELLO 001122334455\n";
  const std::string identify = R"({"type":"identify","data":{}})" "\n";
  // TX不足中の要求は件数を保持し、後続CONFIGを受けても全件に応答する。
  port.output.clear();
  port.writable = 0;
  receive(identify + identify + "CONFIG B1 false\n");
  assert(controller.ready() && port.output.empty());
  port.writable = 256;
  poll();
  assert(port.output == hello + hello);
  port.output.clear();
  now += 1000;
  poll();
  assert(port.output.empty());
  // 受信異常からの再識別も、送信前のCONFIGで応答を失わない。
  port.writable = 0;
  receive("UNKNOWN\nCONFIG B1 false\n");
  assert(controller.ready() && port.output.empty());
  port.writable = 256;
  poll();
  assert(port.output == hello);
  // 接続をまたいで未送信の識別応答を持ち越さない。
  port.output.clear();
  port.writable = 0;
  receive(identify + identify);
  port.connected = false;
  poll();
  now += 100;
  poll();
  port.connected = true;
  port.writable = 256;
  poll();
  assert(port.output == hello);
  receive("LIGHT P1 P1 outgoing\nBUFFER 4\nCONFIG P2 false\n");
  assert(!controller.ready());

  // 全制御ノードのGPIOと仮のLED数。
  const char* nodes[] = {"B1", "B2", "B3", "P1", "P3", "P5", "P7"};
  const std::string pins[] = {"0134567", "013456", "012345", "01", "01", "01", "01"};
  for (size_t n = 0; n < 7; ++n) {
    assert(receive(std::string("CONFIG ") + nodes[n] + " false\n"));
    assert(controller.strip_count() == pins[n].size());
    for (size_t i = 0; i < controller.strip_count(); ++i) {
      assert(controller.route(i).pin == pins[n][i] - '0');
      assert(controller.route(i).count == 1);
    }
  }

  // 分割行とCRLF。CONFIGまでは適用しない。
  receive("CONFIG B");
  assert(controller.strip_count() == 2);
  assert(receive("1 false\r\n"));
  assert(controller.strip_count() == 7);
  expect(pixel(0), 0, 0, 0);
  port.output.clear();
  now += 2000;
  poll();
  assert(port.output.empty());
  receive("BUFFER 2\nLIGHT P1 P1 outgoing\n");
  now += 2000;
  assert(!poll() && controller.ready());
  expect(pixel(0), 0, 255, 0);
  expect(pixel(), 0, 0, 0);

  // BUFFERは0〜4件。経路と独立して保持する。
  const led::Color tower[] = {{0, 0, 255}, {0, 0, 255}, {0, 255, 0},
                            {255, 255, 0}, {255, 0, 0}};
  for (unsigned used = 0; used <= 4; ++used) {
    receive("BUFFER " + std::to_string(used) + "\n");
    expect(pixel(0), tower[used].r, tower[used].g, tower[used].b);
  }
  port.output.clear();
  receive("BUFFER 5\nBUFFER -1\nBUFFER 2x\nBUFFER 00\nBUFFER\nBUFFER 1 extra\n"
          "BUFFER 1 extra extra extra extra\nHELLO 001122334455\n");
  assert(controller.ready() && port.output.empty());
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

  // identifyは全状態で毎回応答し、部分受信は行末まで適用しない。
  receive("BUFFER 4\nLIGHT P1 P1 outgoing\n");
  port.output.clear();
  receive(identify.substr(0, 12));
  assert(controller.ready() && port.output.empty());
  assert(receive(identify.substr(12)));
  assert(!controller.ready() && controller.strip_count() == 0);
  assert(port.output == hello);
  port.output.clear();
  receive(identify + identify);
  assert(port.output == hello + hello);
  port.output.clear();
  now += 999;
  poll();
  assert(port.output.empty());
  ++now;
  poll();
  assert(port.output == hello);
  receive("LIGHT P1 P1 outgoing\nBUFFER 4\nCLEAR\n");
  assert(!controller.ready());

  // 追加フィールド・空白・CRLFを受理し、CONFIGで再送を止める。
  port.output.clear();
  receive(" \t{\"extra\":[1,true,null],\"data\":{\"x\":1},\"type\":\"identify\"}\t\r\n"
          "CONFIG B1 false\n");
  assert(port.output == hello && controller.ready());
  expect(pixel(0), 0, 0, 0);
  port.output.clear();
  now += 2000;
  poll();
  assert(port.output.empty());

  // 不正CONFIG・未知命令・不正JSONは即座に未設定へ戻す。
  const std::string invalid[] = {
    "CONFIG B1\n", "CONFIG B1 0\n", "CONFIG B1 1\n", "CONFIG B1 TRUE\n",
    "CONFIG B1 False\n", "CONFIG B1 true extra\n", "CONFIG B1 false extra\n",
    "CONFIG\n", "CONFIG B9 false\n", "CONFIG B2 extra\n",
    "CONFIG B2 extra extra extra extra\n", "UNKNOWN\n", "config B1\n", "[]\n",
    "{broken}\n", R"({"type":"identify"})" "\n",
    R"({"type":"identify","data":[]})" "\n",
    R"({"type":1,"data":{}})" "\n", R"({"type":"Identify","data":{}})" "\n",
    R"({"type":"identify\u0000x","data":{}})" "\n",
    R"({"type":"identify","data":{}}garbage)" "\n"
  };
  for (const auto& line : invalid) {
    receive("CONFIG B1 false\nBUFFER 4\nLIGHT P1 P1 outgoing\n");
    port.output.clear();
    assert(receive(line));
    assert(!controller.ready() && controller.strip_count() == 0);
    assert(port.output == hello);
    receive("CONFIG B1 false\n");
    expect(pixel(0), 0, 0, 0);
    expect(pixel(), 0, 0, 0);
  }
  // 行末で検出したエラーでは、同じ受信バッチ内の次の正常行を捨てない。
  port.output.clear();
  receive("CONFIG B9 false\nCONFIG B2 false\nBUFFER 2\n");
  assert(controller.ready() && controller.strip_count() == 6 && port.output == hello);
  expect(pixel(0), 0, 255, 0);

  // 96バイト本文はLF/CRLFとも有効。空白だけの行は状態を変えない。
  const std::string config = "CONFIG B1 false";
  const std::string boundary = config + std::string(96 - config.size(), ' ');
  for (const std::string ending : {"\n", "\r\n"}) {
    port.output.clear();
    assert(receive(boundary + ending + "\t \r\n"));
    assert(controller.ready() && port.output.empty());
  }
  assert(!receive(boundary + "\r"));
  assert(controller.ready());
  receive("\n");
  port.output.clear();
  receive(identify.substr(0, identify.size() - 1) +
          std::string(97 - identify.size(), ' ') + "\r\nCONFIG B1 false\n");
  assert(controller.ready() && port.output == hello);

  // 行長超過・使用できない文字は検出時に消灯し、次のLFまで全て破棄。
  const std::string corrupt[] = {
    std::string(97, ' '), std::string("BUFFER 4\0", 9),
    std::string(1, '\x80'), std::string(1, '\x7f'), std::string(1, '\x01'),
    std::string(1, '\v'), "BUFFER\r4", "BUFFER\r\r"
  };
  for (const auto& bytes : corrupt) {
    receive("CONFIG B1 false\nBUFFER 4\nLIGHT P1 P1 outgoing\n");
    port.output.clear();
    assert(receive(bytes));
    assert(!controller.ready() && controller.strip_count() == 0 && port.output == hello);
    port.output.clear();
    now += 500;
    receive("CONFIG B1");
    assert(!controller.ready() && port.output.empty());
    now += 500;
    receive(identify.substr(0, identify.size() - 1));
    assert(!controller.ready() && port.output == hello);
    port.output.clear();
    receive("\nCONFIG B1 false\nBUFFER 3\n");
    assert(controller.ready() && port.output.empty());
    expect(pixel(0), 255, 255, 0);
  }

  // 部分行の1秒タイムアウトも同じ復旧を行い、行の後半は実行しない。
  receive("BUFF");
  now += 999;
  assert(!poll() && controller.ready());
  port.output.clear();
  ++now;
  assert(poll());
  assert(!controller.ready() && port.output == hello);
  port.output.clear();
  now += 1000;
  poll();
  assert(port.output == hello);
  receive("ER 4\nCONFIG B1 false\nBUFFER 3\n");
  assert(controller.ready());
  expect(pixel(0), 255, 255, 0);

  // 未設定中の不正入力では既存のHELLO周期を巻き戻さない。
  receive(identify);
  port.output.clear();
  now += 500;
  receive("UNKNOWN\nCONFIG B9 false\n{broken}\n");
  assert(port.output.empty());
  now += 499;
  poll();
  assert(port.output.empty());
  ++now;
  poll();
  assert(port.output == hello);

  // JSON拡張構文をidentifyと誤認して、未設定中の再送周期を変えない。
  const std::string invalidJson[] = {
    "{'type':'identify','data':{}}\n", "{type:\"identify\",data:{}}\n",
    R"({"type":"identify","data":{},"x":01})" "\n",
    R"({"type":"identify","data":{},"x":+1})" "\n",
    R"({"type":"identify","data":{},"x":.1})" "\n",
    R"({"type":"identify","data":{},"x":1.})" "\n",
    R"({"type":"identify","data":{},"x":1e})" "\n",
    R"({"type":"identify","data":{},"x":"\u0)" "\n",
    R"({"type":"identify","data":{},"x":"\)" "\n",
    R"({"type":"identify","data":{},"x":"unterminated)" "\n",
    R"({"type":"identify","data":{},"x":"\u00:0"})" "\n",
    "{\"type\":\"identify\",\"data\":{},\"x\":\"raw\ttab\"}\n"
  };
  port.output.clear();
  now += 500;
  for (const auto& line : invalidJson) receive(line);
  assert(!controller.ready() && port.output.empty());
  receive(R"({"type":"identi\u0066y","data":{},"x":[-0.1e+2,"'\"\\",{}]})" "\n");
  assert(!controller.ready() && port.output == hello);

  // CRLFのCRだけを受け取った状態にも部分行のタイムアウトを適用する。
  receive("CONFIG B1 false\n\r");
  port.output.clear();
  now += 1000;
  assert(poll() && !controller.ready() && port.output == hello);
  receive("CONFIG B1 false\n");
  assert(!controller.ready());
  receive("CONFIG B1 false\n");
  assert(controller.ready());

  // 全NODEの確認表示はGPIO0・1だけを使い、通常命令を混ぜない。
  auto dark = [&] {
    for (size_t strip = 0; strip < controller.strip_count(); ++strip) expect(pixel(strip), 0, 0, 0);
  };
  for (const char* node : nodes) {
    now += 137;
    const uint32_t started = now;
    const led::Color color = led::node_color(led::node_id(node));
    const bool bot = node[0] == 'B';
    port.output.clear();
    assert(receive(std::string("CONFIG ") + node + " true\n"));
    assert(controller.ready() && port.output.empty());
    auto visible = [&](unsigned intensity) {
      for (size_t strip = 0; strip < controller.strip_count(); ++strip) {
        const unsigned pin = controller.route(strip).pin;
        if (bot && pin == 0) expect(pixel(strip), 255, 0, 0);
        else if (pin < 2) expect(pixel(strip), (color.r * intensity + 127) / 255,
                                (color.g * intensity + 127) / 255,
                                (color.b * intensity + 127) / 255);
        else expect(pixel(strip), 0, 0, 0);
      }
    };
    receive(std::string("BUFFER 0\nLIGHT ") + controller.route(controller.strip_count() - 1).neighbor +
            " P6 outgoing\nCLEAR extra\nBUFFER 5\nLIGHT P0 P1 outgoing\n");
    visible(255);
    now = started + 10;
    poll();
    visible(128);
    now = started + 499;
    visible(0);
    now = started + 500;
    poll();
    dark();
    now = started + 1000;
    poll();
    visible(255);
    now += 10;
    receive("CLEAR\n");
    assert(controller.ready());
    dark();
    now = started + 1999;
    dark();
    now = started + 2000;
    poll();
    visible(255);
    // 過去の周期を再生せず、現在の周期の経過時間へ直接追いつく。
    now = started + 5500;
    poll();
    dark();
    now = started + 6020;
    poll();
    visible(64);
    receive(std::string("CONFIG ") + node + " true\n");
    visible(255);
    receive(std::string("CONFIG ") + node + " false\n");
    dark();
    now += 1000;
    poll();
    dark();
    assert(port.output.empty());
  }

  // CLEARは消灯期間中も起点を動かさず、次の周期で再開する。
  receive("CONFIG B1 true\n");
  now += 600;
  receive("CLEAR\n");
  dark();
  now += 400;
  poll();
  expect(pixel(0), 255, 0, 0);
  // 再識別・異常・切断も、確認表示から通常の未設定状態へ戻す。
  receive(identify);
  assert(!controller.ready());
  receive("CONFIG B1 true\nUNKNOWN\n");
  assert(!controller.ready());
  receive("CONFIG B1 true\n");
  port.connected = false;
  poll();
  now += 100;
  assert(poll() && !controller.ready());
  port.connected = true;
  poll();
  receive("CONFIG B1 false\n");
  dark();

  // 確認周期もmillis()の周回を跨いで継続する。
  now = std::numeric_limits<uint32_t>::max() - 4;
  receive("CONFIG P1 true\n");
  now += 10;
  poll();
  expect(pixel(0), 128, 0, 0);
  now += 990;
  poll();
  expect(pixel(0), 255, 0, 0);

  // 再CONFIGでは残像とタワーを初期化。プレイヤー制御ノードのBUFFERは無視。
  receive("LIGHT P1 P1 outgoing\nCONFIG B1 false\n");
  expect(pixel(), 0, 0, 0);
  expect(pixel(0), 0, 0, 0);
  receive("CONFIG P1 false\nBUFFER 4\nLIGHT P8 P4 outgoing\n");
  expect(pixel(0), 0, 0, 0);
  expect(pixel(1), 0, 255, 0);

  // 同時点灯数が上限を超えても既存演出を保持し、終了後は空きを再利用。
  receive("CLEAR\n");
  for (size_t i = 0; i < led::Controller::MAX_LIGHTS; ++i) receive("LIGHT P2 P1 outgoing\n");
  receive("LIGHT P8 P6 outgoing\n");
  expect(pixel(0), 255, 0, 0);
  expect(pixel(1), 0, 0, 0);
  now += 100;
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

  // 満杯でもBUFFER・CONFIG・CLEARを受け付け、全枠を解放する。
  receive("CONFIG B1 false\n");
  for (size_t i = 0; i < led::Controller::MAX_LIGHTS; ++i) receive("LIGHT P1 P1 outgoing\n");
  receive("BUFFER 2\nCLEAR\nLIGHT P2 P6 incoming\n");
  expect(pixel(0), 0, 0, 0);
  expect(pixel(2), 0, 0, 255);

  // 短いCDC未接続判定は許容し、100ms継続したら行破棄状態も初期化。
  port.connected = false;
  assert(!poll());
  now += 99;
  assert(!poll());
  assert(controller.ready());
  port.connected = true;
  poll();
  assert(controller.ready());
  receive("partial");
  port.connected = false;
  poll();
  now += 100;
  assert(poll());
  assert(!controller.ready() && controller.strip_count() == 0);
  port.connected = true;
  port.output.clear();
  poll();
  assert(port.output == "HELLO 001122334455\n");
  receive("CONFIG B1 false\n");
  receive(std::string(97, 'x'));
  assert(!controller.ready());
  port.connected = false;
  poll();
  now += 100;
  poll();
  port.connected = true;
  port.output.clear();
  poll();
  assert(port.output == hello);
  receive("CONFIG B1 false\n");
  assert(controller.ready());

  // millis()の周回を跨いでもアニメーションの経過時間が正しい。
  now = std::numeric_limits<uint32_t>::max() - 4;
  receive("CONFIG P1 false\nLIGHT P2 P1 outgoing\n");
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
