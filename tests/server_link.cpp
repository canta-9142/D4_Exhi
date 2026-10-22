#include <cassert>
#include <iostream>
#include <string>
#include "../src/player/server_link.hpp"

struct TestPort {
  bool connected = true;
  std::string input;
  std::string output;
  size_t position = 0;
  int writable = 256;

  explicit operator bool() const { return connected; }
  int available() const { return input.size() - position; }
  int availableForWrite() const { return writable; }
  int read() { return available() ? static_cast<unsigned char>(input[position++]) : -1; }
  size_t write(const uint8_t* bytes, size_t size) {
    output.append(reinterpret_cast<const char*>(bytes), size);
    return size;
  }
};

int main() {
  ServerLink link;
  TestPort port;
  PacketQueue queue;
  NodeId self = NodeId::None;
  uint32_t now = 0;
  auto poll = [&] { return link.poll(port, "001122334455", queue, self, now); };
  auto receive = [&](const std::string& bytes) {
    port.input = bytes;
    port.position = 0;
    bool reset = false;
    while (port.available()) reset |= poll();
    return reset;
  };
  const std::string config = R"({"type":"config","data":{"node":1}})" "\n";
  const std::string reset = R"({"type":"reset","data":{"packets":[{"pid":10,"dst":2}]}})" "\n";
  const std::string ok = R"({"type":"result","data":{"ok":true}})" "\n";
  const std::string push = R"({"type":"push","data":{"pid":11,"dst":3}})" "\n";

  poll();
  assert(port.output == R"({"type":"hello","data":{"uid":"001122334455"}})" "\n");
  assert(!link.ready());
  receive(config.substr(0, 18));
  assert(self == NodeId::None);
  receive(config.substr(18));
  assert(self == NodeId::P1 && !link.ready());
  assert(receive(reset));
  assert(link.ready() && queue.size() == 1 && queue.at(0)->id == 10);

  receive(R"({"type":"push")");
  now += ServerLink::RECEIVE_TIMEOUT_MS;
  port.output.clear();
  poll();
  assert(!link.ready() && queue.size() == 1);
  assert(port.output.find("\"resync\"") != std::string::npos);
  receive(reset);

  port.output.clear();
  assert(link.send(port, queue, NodeId::P2, now));
  assert(port.output == R"({"type":"send","data":{"pid":10,"to":2}})" "\n");
  assert(!link.send(port, queue, NodeId::P2, now));
  receive(push);
  assert(queue.size() == 2 && link.pending());
  receive(R"({"type":"result","data":{"ok":false}})" "\n");
  assert(queue.size() == 2 && !link.pending());
  assert(link.send(port, queue, NodeId::P2, now));
  receive(ok);
  assert(queue.size() == 1 && queue.at(0)->id == 11 && !link.pending());
  receive(ok); // 未要求の応答では削除しない。
  assert(queue.size() == 1);

  assert(link.send(port, queue, NodeId::P2, now));
  now += ServerLink::SEND_TIMEOUT_MS;
  port.output.clear();
  poll();
  assert(!link.ready() && !link.pending() && queue.size() == 1);
  assert(port.output == R"({"type":"resync","data":{}})" "\n");
  receive(ok); // タイムアウトした要求の遅延応答も無視。
  assert(queue.size() == 1);
  receive(reset);
  assert(link.ready() && queue.at(0)->id == 10);

  const std::string invalid[] = {
    "garbage\n", "[]\n", R"({"type":"push","data":{"pid":-1,"dst":2}})" "\n",
    R"({"type":"push","data":{"pid":65536,"dst":2}})" "\n",
    R"({"type":"push","data":{"pid":12,"dst":12}})" "\n",
    R"({"type":"push","data":{"pid":10,"dst":2}})" "\n",
    R"({"type":"result","data":{"ok":1}})" "\n",
    R"({"type":"reset","data":{"packets":[{"pid":20,"dst":2},{}]}})" "\n",
    R"({"type":"reset","data":{"packets":[{"pid":20,"dst":2},{"pid":20,"dst":3}]}})" "\n",
    std::string(ServerLink::MAX_LINE_BYTES + 1, 'x') + "\n"
  };
  for (const auto& message : invalid) {
    port.output.clear();
    receive(message);
    assert(!link.ready() && queue.size() == 1 && queue.at(0)->id == 10);
    assert(port.output.find("\"resync\"") != std::string::npos);
    assert(receive(reset) && link.ready());
  }

  std::string full = R"({"type":"reset","data":{"packets":[)";
  for (size_t i = 0; i < PacketQueue::capacity(); ++i) {
    if (i) full += ',';
    full += R"({"pid":)" + std::to_string(i) + R"(,"dst":8})";
  }
  full += "]}}\n";
  assert(receive(full));
  assert(link.ready() && queue.size() == PacketQueue::capacity());
  assert(queue.at(1023)->id == 1023);
  receive(R"({"type":"push","data":{"pid":2000,"dst":1}})" "\n");
  assert(!link.ready() && queue.size() == PacketQueue::capacity());

  full.insert(full.size() - 4, R"(,{"pid":2000,"dst":8})");
  assert(!receive(full));
  assert(queue.size() == PacketQueue::capacity());
  receive(reset);
  port.writable = 0;
  assert(!link.send(port, queue, NodeId::P2, now) && !link.pending());
  port.writable = 256;

  port.connected = false;
  poll();
  now += ServerLink::DISCONNECT_GRACE_MS - 1;
  port.connected = true;
  poll();
  assert(link.ready()); // 短時間の接続判定の揺れでは設定を捨てない。
  port.connected = false;
  poll();
  now += ServerLink::DISCONNECT_GRACE_MS;
  poll();
  assert(!link.ready());
  port.connected = true;
  now += ServerLink::RETRY_MS;
  port.output.clear();
  poll();
  assert(port.output.find("\"hello\"") != std::string::npos);
  receive(config + reset + push);
  assert(link.ready() && queue.size() == 2);
  receive(R"({"type":"reset","data":{"packets":[]}})" "\r\n");
  assert(link.ready() && queue.size() == 0);
  assert(!link.send(port, queue, NodeId::P2, now));
  std::cout << "PASS: handshake, split/batched lines, reset/push, send/result, send/receive timeouts, validation, capacity, reconnect\n";
}
