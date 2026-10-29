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
  size_t writeLimit = 256;

  explicit operator bool() const { return connected; }
  int available() const { return input.size() - position; }
  int availableForWrite() const { return writable; }
  int read() { return available() ? static_cast<unsigned char>(input[position++]) : -1; }
  size_t write(const uint8_t* bytes, size_t size) {
    size = std::min(size, writeLimit);
    output.append(reinterpret_cast<const char*>(bytes), size);
    return size;
  }
};

struct TestContext {
  ServerLink link;
  TestPort port;
  PacketQueue queue;
  NodeId self = NodeId::None;
  uint32_t now = 0;

  bool poll() { return link.poll(port, "001122334455", queue, self, now); }

  bool receive(const std::string& bytes) {
    port.input = bytes;
    port.position = 0;
    bool applied = false;
    while (port.available()) applied |= poll();
    return applied;
  }
};

static std::string message(const std::string& type, const std::string& data) {
  return "{\"type\":\"" + type + "\",\"data\":" + data + "}\n";
}

static std::string reset(const std::string& sync, const std::string& packets) {
  return message("reset", "{\"sync\":\"" + sync + "\",\"packets\":" + packets + "}");
}

static std::string result(const std::string& sync, uint32_t rid, bool ok = true) {
  return message("result", "{\"sync\":\"" + sync + "\",\"rid\":" + std::to_string(rid) +
                 ",\"ok\":" + (ok ? "true}" : "false}"));
}

static void test_status_and_mode() {
  TestContext ctx;
  const std::string sync = "0000000000000000000000000000000A";
  const std::string fifo = reset(sync, R"([{"pid":10,"dst":2}])");
  const std::string manual = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const std::string automatic = message("config", R"({"node":1,"mode":"auto","preview":false})");
  const std::string playing = message("phase", R"({"phase":"Playing"})");
  const std::string status = message("status", R"({"score":23,"remaining":87,"ready":true,"countdown":0})");
  const std::string maximum = message("status", R"({"score":2147483647,"remaining":120,"ready":false,"countdown":0})");
  const std::string hello = message("hello", R"({"uid":"001122334455"})");
  const std::string resync = message("resync", "{}");
  assert(ctx.link.mode() == ControlMode::Auto && !ctx.link.status().known);
  ctx.receive(status);
  assert(!ctx.link.status().known); // 未設定のSTATUSは保存しない。
  ctx.receive(automatic + fifo + playing + maximum);
  assert(ctx.link.fifo_synced() && !ctx.link.awaiting_status() && ctx.link.mode() == ControlMode::Auto);
  assert(ctx.link.status().score == INT32_MAX && ctx.link.status().remaining == 120);
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));

  ctx.receive(manual + status); // RESET前のSTATUSは保存するが、同期の取得完了にはしない。
  assert(!ctx.link.fifo_synced() && ctx.link.status().known && ctx.link.awaiting_status());
  ctx.receive(fifo + playing);
  assert(ctx.link.fifo_synced() && ctx.link.awaiting_status());
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now)); // STATUS待ちでもSENDは許可。
  ctx.receive(result(sync, 0, false));
  ctx.now = ServerLink::STATUS_TIMEOUT_MS - 1;
  ctx.receive(playing + message("push", R"({"pid":11,"dst":3})") + fifo);
  assert(ctx.link.fifo_synced() && ctx.link.awaiting_status());
  ctx.port.output.clear();
  ++ctx.now;
  ctx.poll(); // PUSH、PHASE再通知、通常RESETで期限が延長されていない。
  assert(ctx.port.output == resync && !ctx.link.fifo_synced());
  assert(ctx.link.status().known && ctx.link.status().score == 23 && ctx.link.status().ready);
  ctx.receive(status); // 再同期のRESETより前に来た値では取得完了にしない。
  assert(ctx.link.awaiting_status());
  ctx.receive(fifo);
  ctx.now += ServerLink::STATUS_TIMEOUT_MS - 1;
  ctx.receive(status); // 同じ値でも、新しいRESETの後なら取得完了。
  assert(!ctx.link.awaiting_status() && ctx.link.fifo_synced());
  ctx.port.output.clear();
  ctx.now += 60000;
  ctx.poll();
  assert(ctx.port.output.empty()); // 通常の無通信状態にはタイムアウトを設けない。
  ctx.receive(fifo);
  assert(!ctx.link.awaiting_status() && ctx.link.status().known); // 通常RESETは取得待ちを増やさない。

  // STATUSは加点差分ではなく絶対値。0秒でもフェーズを独自に変更しない。
  ctx.receive(message("status", R"({"score":0,"remaining":0,"ready":false,"countdown":0})"));
  assert(ctx.link.status().score == 0 && ctx.link.status().remaining == 0 && !ctx.link.status().ready);
  assert(ctx.link.phase() == GamePhase::Playing && ctx.link.mode() == ControlMode::Manual);
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.now += ServerLink::SEND_TIMEOUT_MS - 1;
  ctx.receive(maximum);
  assert(ctx.link.pending() && ctx.queue.size() == 1);
  ctx.port.output.clear();
  ++ctx.now;
  ctx.poll(); // STATUS更新はSENDの応答タイマーを変更しない。
  assert(!ctx.link.pending() && ctx.port.output == resync);

  const std::string badStatus[] = {
    R"({"remaining":120,"ready":true,"countdown":0})", R"({"score":0,"ready":true,"countdown":0})",
    R"({"score":0,"remaining":120,"countdown":0})", R"({"score":-1,"remaining":120,"ready":true,"countdown":0})",
    R"({"score":2147483648,"remaining":120,"ready":true,"countdown":0})",
    R"({"score":1.0,"remaining":120,"ready":true,"countdown":0})",
    R"({"score":true,"remaining":120,"ready":true,"countdown":0})",
    R"({"score":0,"remaining":-1,"ready":true,"countdown":0})", R"({"score":0,"remaining":121,"ready":true,"countdown":0})",
    R"({"score":0,"remaining":"120","ready":true,"countdown":0})", R"({"score":0,"remaining":1.0,"ready":true,"countdown":0})",
    R"({"score":0,"remaining":120,"ready":1,"countdown":0})", R"({"score":0,"remaining":120,"ready":"true","countdown":0})"
  };
  for (const auto& fields : badStatus) {
    ctx.receive(fifo + maximum);
    ctx.port.output.clear();
    ctx.receive(message("status", fields));
    assert(ctx.port.output == resync && !ctx.link.fifo_synced() && ctx.self == NodeId::P1);
    assert(ctx.link.status().known && ctx.link.status().score == INT32_MAX &&
           ctx.link.status().remaining == 120 && !ctx.link.status().ready && ctx.queue.size() == 1);
  }

  const std::string badConfigOrJson[] = {
    message("config", R"({"node":1})"), message("config", R"({"mode":"manual","preview":false})"),
    message("config", R"({"node":true,"mode":"manual","preview":false})"),
    message("config", R"({"node":9,"mode":"manual","preview":false})"),
    message("config", R"({"node":1,"mode":"MANUAL"})"),
    message("config", R"({"node":1,"mode":true})"),
    message("config", R"({"node":1,"mode":"manual\u0000x"})"),
    message("config", R"({"node":1,"mode":"manual\uD800"})"),
    message("config", R"({"node":1,"mode":"man\uD800ual"})"),
    message(R"(config\uD800)", R"({"node":1,"mode":"manual","preview":false})"),
    message("phase", R"({"phase":"Play\uD800ing"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"\uD800":0})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uDC00"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uDFFF"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uDC00\uD800"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uD800\u0041"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uD800\uD800\uDC00"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uD800\n\uDC00"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uD800あ\uDC00"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uD800\uDC0"})"),
    message("config", "[]"), "garbage\n", "{\n",
    "{'type':'config','data':{'node':1,'mode':'manual'}}\n",
    "{type:\"config\",data:{node:1,mode:\"manual\"}}\n",
    message("config", R"({"node":+1,"mode":"manual","preview":false})"),
    message("config", R"({"node":01,"mode":"manual","preview":false})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":.1})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":1.})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":1.e2})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":1e+})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":truex})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\'"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"\uZZZZ"})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,true:0})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,0:0})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":"raw)" + std::string(1, '\t') + "tab\"}"),
    message("config", R"({"node":1,"mode":"manual","preview":false,})"),
    message("config", R"({"node":1,"mode":"manual","preview":false,"extra":[1,]})"),
    manual.substr(0, manual.size() - 1) + "garbage\n",
    manual.substr(0, manual.size() - 1) + automatic,
    manual.substr(0, manual.size() - 1) + std::string(1, '\0') + "\n"
  };
  for (const auto& bytes : badConfigOrJson) {
    ctx.receive(manual + fifo + playing + status);
    ctx.port.output.clear();
    ctx.receive(bytes);
    assert(ctx.port.output == hello && ctx.self == NodeId::None && ctx.queue.size() == 0);
    assert(ctx.link.mode() == ControlMode::Auto && ctx.link.phase() == GamePhase::Unknown);
    assert(!ctx.link.status().known && !ctx.link.fifo_synced() && !ctx.link.pending());
  }

  // 正当なJSONの空白、エスケープ、負数、小数、指数、未知の追加フィールドは許可する。
  ctx.receive(" \t" + message("config",
      R"({"node":1,"mode":"man\u0075al","preview":false,"extra":[-0,-12,0.5,1e2,1E-2,1e+2,true,false,null,"apostrophe's", "\"\\\/\b\f\n\r\t\u3042"]})"));
  ctx.receive(fifo + playing + status);
  assert(ctx.self == NodeId::P1 && ctx.link.mode() == ControlMode::Manual && ctx.link.fifo_synced());

  // 改行を待たず、上限超過を検出した時点で手動入力と旧表示を無効化する。
  ctx.receive(manual + fifo + playing + status);
  ctx.receive(std::string(ServerLink::MAX_LINE_BYTES + 1, 'x'));
  assert(ctx.link.mode() == ControlMode::Auto && ctx.self == NodeId::None && ctx.queue.size() == 0);
  ctx.receive("\n" + manual + fifo + playing + status);
  assert(ctx.link.fifo_synced() && ctx.link.status().known);
  ctx.receive(R"({"type":"config")");
  ctx.now += ServerLink::RECEIVE_TIMEOUT_MS;
  ctx.port.output.clear();
  ctx.poll();
  assert(ctx.port.output == hello && ctx.link.mode() == ControlMode::Auto && !ctx.link.status().known);

  ctx.now = UINT32_MAX - 1000;
  ctx.receive(manual + fifo + playing);
  ctx.now += ServerLink::STATUS_TIMEOUT_MS - 1;
  ctx.poll();
  assert(ctx.link.fifo_synced());
  ++ctx.now;
  ctx.poll();
  assert(!ctx.link.fifo_synced()); // STATUS期限もmillis()の周回をまたげる。
}

static void test_utf8_validation() {
  TestContext ctx;
  const std::string config = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const std::string fifo = reset("0000000000000000000000000000000A", R"([{"pid":10,"dst":2}])");
  const std::string status = message("status", R"({"score":1,"remaining":120,"ready":false,"countdown":0})");
  const std::string hello = message("hello", R"({"uid":"001122334455"})");
  const auto with_text = [](const std::string& text) {
    return message("config", "{\"node\":1,\"mode\":\"manual\",\"preview\":false,\"extra\":\"" + text + "\"}");
  };
  const std::string invalid[] = {
    "\x80", "\xBF", "\xC0\x80", "\xC1\xBF", "\xC2", "\xC2" "x",
    "\xE0\x80\x80", "\xED\xA0\x80", "\xE2\x82", "\xE2\xC0\xA0",
    "\xF0\x80\x80\x80", "\xF0\x90\x80", "\xF4\x90\x80\x80",
    "\xF5\x80\x80\x80", "\xFE", "\xFF"
  };
  for (const auto& text : invalid) {
    ctx.receive(config + fifo + status);
    ctx.port.output.clear();
    ctx.receive(with_text(text));
    assert(ctx.port.output == hello && ctx.self == NodeId::None && ctx.queue.size() == 0);
    assert(ctx.link.mode() == ControlMode::Auto && !ctx.link.status().known);
  }
  const std::string valid[] = {
    "\xC2\x80", "\xDF\xBF", "\xE0\xA0\x80", "\xED\x9F\xBF", "\xEE\x80\x80",
    "\xEF\xBF\xBF", "\xF0\x90\x80\x80", "\xF4\x8F\xBF\xBF", u8"準備完了🐟"
  };
  for (const auto& text : valid) {
    ctx.receive(with_text(text) + fifo + status);
    assert(ctx.self == NodeId::P1 && ctx.link.mode() == ControlMode::Manual && ctx.link.fifo_synced());
  }
  // マルチバイト文字がUSBの分割境界をまたいでも、行の完成後に検証する。
  ctx.receive(message("identify", "{}"));
  const std::string bytes = with_text(u8"🐟");
  const size_t cut = bytes.find('\xF0') + 2;
  ctx.receive(bytes.substr(0, cut));
  assert(ctx.self == NodeId::None);
  ctx.receive(bytes.substr(cut) + fifo + status);
  assert(ctx.self == NodeId::P1 && ctx.link.fifo_synced());
}

static void test_oversized_line_drop() {
  TestContext ctx;
  const std::string config = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const std::string fifo = reset("0000000000000000000000000000000A", R"([{"pid":10,"dst":2}])");
  ctx.receive(config + fifo);
  ctx.receive(std::string(ServerLink::MAX_LINE_BYTES + 1, 'x'));
  for (int attempt = 0; attempt < 3; ++attempt) {
    ctx.now += ServerLink::RECEIVE_TIMEOUT_MS;
    ctx.poll();
    // 同じ超過行の後半。LFが来るまではCONFIGとして解釈しない。
    ctx.receive(config.substr(0, config.size() - 1));
    assert(ctx.self == NodeId::None && ctx.link.mode() == ControlMode::Auto);
  }
  ctx.receive("\r\n");
  assert(ctx.self == NodeId::None && ctx.queue.size() == 0);
  ctx.receive(config + fifo);
  assert(ctx.self == NodeId::P1 && ctx.link.fifo_synced());

  // 切断確定時は破棄状態も初期化し、新しい接続の最初の行から受け付ける。
  ctx.receive(std::string(ServerLink::MAX_LINE_BYTES + 1, 'x'));
  ctx.port.connected = false;
  ctx.poll();
  ctx.now += ServerLink::DISCONNECT_GRACE_MS;
  ctx.poll();
  ctx.port.connected = true;
  ctx.receive(config + fifo);
  assert(ctx.self == NodeId::P1 && ctx.link.fifo_synced());
}

static void test_unicode_escapes_and_nesting() {
  TestContext ctx;
  const std::string fifo = reset("0000000000000000000000000000000A", R"([{"pid":10,"dst":2}])");
  const std::string validFields[] = {
    R"({"node":1,"mode":"manual","preview":false,"extra":"\uD800\uDC00\uDBFF\uDFFF"})",
    R"({"node":1,"mode":"manual","preview":false,"extra":"\ud83d\udc1f\uD7FF\uE000"})",
    R"({"node":1,"mode":"manual","preview":false,"extra":"\\uD800"})",
    R"({"node":1,"mode":"manual","preview":false,"\uD800\uDC00":0})",
    R"({"node":1,"mode":"manual","preview":false,"extra":{"a":{}}})",
    R"({"node":1,"mode":"manual","preview":false,"extra":[[]]})",
    R"({"node":1,"mode":"manual","preview":false,"extra":[{}]})"
  };
  for (const auto& fields : validFields) {
    ctx.receive(message("identify", "{}"));
    ctx.receive(message("config", fields) + fifo);
    assert(ctx.self == NodeId::P1 && ctx.link.mode() == ControlMode::Manual && ctx.link.fifo_synced());
  }
  const std::string tooDeep[] = {
    R"({"node":1,"mode":"manual","preview":false,"extra":{"a":{"b":{}}}})",
    R"({"node":1,"mode":"manual","preview":false,"extra":[[[]]]})",
    R"({"node":1,"mode":"manual","preview":false,"extra":[{"a":[]}]})"
  };
  for (const auto& fields : tooDeep) {
    ctx.receive(message("config", R"({"node":1,"mode":"manual","preview":false})") + fifo);
    ctx.port.output.clear();
    ctx.receive(message("config", fields));
    assert(ctx.port.output == message("hello", R"({"uid":"001122334455"})"));
    assert(ctx.self == NodeId::None && ctx.queue.size() == 0 && ctx.link.mode() == ControlMode::Auto);
  }
}

static void test_partial_writes() {
  const std::string sync = "0000000000000000000000000000000A";
  const std::string config = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const std::string fifo = reset(sync, R"([{"pid":10,"dst":2}])");
  const std::string playing = message("phase", R"({"phase":"Playing"})");
  const std::string status = message("status", R"({"score":0,"remaining":120,"ready":false,"countdown":0})");
  const std::string hello = message("hello", R"({"uid":"001122334455"})");
  const std::string resync = message("resync", "{}");
  const std::string send = message("send", "{\"sync\":\"" + sync + "\",\"rid\":0,\"pid\":10,\"to\":2}");

  // 改行直前を含む全分割位置で、追加SENDを止め、未送信部分だけを続送する。
  for (size_t cut = 1; cut < send.size(); ++cut) {
    TestContext ctx;
    ctx.receive(config + fifo + playing + status);
    ctx.port.output.clear();
    ctx.port.writeLimit = cut;
    assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
    assert(ctx.link.pending() && ctx.port.output == send.substr(0, cut));
    assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
    ctx.port.writable = 0;
    ctx.poll();
    assert(ctx.port.output == send.substr(0, cut) && ctx.link.pending());
    ctx.port.writable = 1;
    ctx.port.writeLimit = 1;
    ctx.poll();
    assert(ctx.port.output == send.substr(0, cut + 1));
    ctx.port.writable = 256;
    ctx.port.writeLimit = 256;
    ctx.poll();
    assert(ctx.port.output == send && ctx.link.pending() && ctx.queue.size() == 1);
    ctx.receive(result(sync, 0));
    assert(ctx.queue.size() == 0 && !ctx.link.pending());
  }

  TestContext ctx;
  ctx.port.writeLimit = 7;
  ctx.poll();
  assert(ctx.port.output == hello.substr(0, 7));
  ctx.port.writeLimit = 256;
  ctx.poll();
  assert(ctx.port.output == hello); // HELLOも同じ送信経路。
  ctx.receive(config + fifo + playing + status);
  ctx.port.output.clear();
  ctx.port.writeLimit = 0;
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  assert(!ctx.link.pending() && ctx.port.output.empty());
  ctx.port.writeLimit = 12;
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  const std::string send1 = message("send", "{\"sync\":\"" + sync + "\",\"rid\":1,\"pid\":10,\"to\":2}");
  ctx.port.writable = 0;
  ctx.now += ServerLink::SEND_TIMEOUT_MS;
  ctx.poll();
  assert(!ctx.link.pending() && !ctx.link.fifo_synced() && ctx.queue.size() == 1);
  ctx.port.writable = 256;
  ctx.poll(); // SENDの残りを完送するまではRESYNCを混ぜない。
  assert(ctx.port.output == send1.substr(0, 24));
  ctx.port.writeLimit = 0;
  ctx.poll();
  assert(ctx.port.output == send1.substr(0, 24));
  ctx.port.writeLimit = 256;
  ctx.poll();
  assert(ctx.port.output == send1 + resync);
  ctx.receive(fifo + status);

  ctx.port.output.clear();
  ctx.port.writeLimit = 7;
  ctx.receive(message("phase", R"({"phase":"invalid"})"));
  assert(ctx.port.output == resync.substr(0, 7));
  ctx.port.connected = false;
  ctx.poll();
  ctx.now += ServerLink::DISCONNECT_GRACE_MS - 1;
  ctx.port.connected = true;
  ctx.port.writeLimit = 256;
  ctx.poll();
  assert(ctx.port.output == resync); // 短時間の接続判定の揺れでは送信末尾を保持。
  ctx.receive(fifo + status);

  // 再識別でも既に送った接頭辞を放置せず、旧SENDの末尾→HELLOの順に送る。
  ctx.port.output.clear();
  ctx.port.writeLimit = 12;
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  const std::string send2 = message("send", "{\"sync\":\"" + sync + "\",\"rid\":2,\"pid\":10,\"to\":2}");
  ctx.receive(message("identify", "{}"));
  assert(!ctx.link.pending() && ctx.self == NodeId::None);
  ctx.port.writeLimit = 256;
  ctx.poll();
  assert(ctx.port.output == send2 + hello);

  // 接続が失われた場合だけ旧セッションの未送信部分を破棄する。
  ctx.receive(config + fifo + playing + status);
  ctx.port.output.clear();
  ctx.port.writeLimit = 12;
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.port.connected = false;
  ctx.poll();
  ctx.now += ServerLink::DISCONNECT_GRACE_MS;
  ctx.poll();
  ctx.port.connected = true;
  ctx.port.output.clear();
  ctx.port.writeLimit = 256;
  ctx.poll();
  assert(ctx.port.output == hello && !ctx.link.pending() && ctx.self == NodeId::None);
}

static void test_ready() {
  TestContext ctx;
  const std::string syncA = "0000000000000000000000000000000A";
  const std::string syncB = "0000000000000000000000000000000B";
  const auto config = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const auto prep = message("phase", R"({"phase":"Preparing"})");
  const auto playing = message("phase", R"({"phase":"Playing"})");
  const auto no = message("status", R"({"score":0,"remaining":120,"ready":false,"countdown":0})");
  const auto yes = message("status", R"({"score":0,"remaining":120,"ready":true,"countdown":0})");
  const auto readyA = message("ready", "{\"sync\":\"" + syncA + "\"}");
  const auto readyB = message("ready", "{\"sync\":\"" + syncB + "\"}");
  const auto resync = message("resync", "{}");
  const auto step = [&](uint8_t mask, uint32_t elapsed = 0) {
    ctx.now += elapsed;
    ctx.poll();
    return ctx.link.update_ready(ctx.port, mask, ctx.now);
  };
  ctx.receive(config + reset(syncA, R"([{"pid":10,"dst":2}])") + prep + no);
  ctx.port.output.clear();
  assert(!step(9, 2000));
  step(0);
  step(1);
  step(9, 100);
  assert(!step(15, 1999) && ctx.link.ready_progress_ms() == 1999);
  assert(step(9, 1) && ctx.port.output == readyA && ctx.link.ready_pending());
  assert(ctx.queue.size() == 1 && ctx.link.mode() == ControlMode::Manual && ctx.link.phase() == GamePhase::Preparing);
  ctx.port.output.clear();
  ctx.receive(no + prep); // falseや同一PHASEで確認待ちの期限を延ばさない。
  assert(!step(9, 2999) && ctx.link.ready_pending() && ctx.port.output.empty());
  step(9, 1);
  assert(!ctx.link.ready_pending() && !ctx.link.fifo_synced() && ctx.port.output == resync);
  ctx.receive(reset(syncB, R"([{"pid":10,"dst":2}])") + prep + no);
  ctx.port.output.clear();
  assert(!step(9, 2000));
  step(0);
  step(9);
  assert(step(9, 2000) && ctx.port.output == readyB);
  ctx.receive(yes);
  assert(!ctx.link.ready_pending() && ctx.link.status().ready);
  ctx.port.output.clear();
  step(0);
  assert(!step(9, 10000) && ctx.port.output.empty());
  ctx.receive(no);
  step(0);
  step(9);
  ctx.receive(playing + prep); // 同じpoll内で戻っても長押しを解除する。
  assert(!step(9, 2000));
  step(0);
  step(9);
  ctx.receive(reset(syncB, R"([{"pid":10,"dst":2}])") + no);
  assert(!step(9, 2000));
  step(0);
  step(9);
  assert(step(9, 2000));
  ctx.receive(playing);
  assert(!ctx.link.ready_pending());
  ctx.port.output.clear();
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  assert(ctx.port.output.find("\"rid\":0") != std::string::npos); // READYはRIDを消費しない。
  ctx.receive(result(syncB, 0));
  ctx.receive(config + reset(syncA, "[]") + prep); // STATUS未取得では要求しない。
  ctx.port.output.clear();
  step(0);
  assert(!step(9, 2000));
  ctx.receive(no);
  step(0);
  step(9);
  ctx.port.writeLimit = 5;
  assert(step(9, 2000));
  for (unsigned i = 0; i < 30; ++i) ctx.poll();
  assert(ctx.port.output == readyA && ctx.link.ready_pending());
  ctx.port.writeLimit = 256;
  ctx.receive(message("config", R"({"node":1,"mode":"auto","preview":false})") + reset(syncA, "[]") + prep + no);
  ctx.port.output.clear();
  step(0);
  assert(!step(9, 3000) && !ctx.link.ready_pending() && ctx.port.output.empty());
  ctx.receive(config + reset(syncA, "[]") + prep + no);
  step(0);
  step(9);
  ctx.port.writable = 0;
  assert(!step(9, 2000) && !ctx.link.fifo_synced());
  ctx.port.writable = 256;
  ctx.poll();
  assert(ctx.port.output == resync);
  ctx.receive(config + reset(syncA, "[]") + prep + no);
  step(0);
  step(9);
  assert(step(9, 2000) && ctx.link.ready_pending());
  ctx.port.connected = false;
  ctx.poll();
  ctx.now += ServerLink::DISCONNECT_GRACE_MS;
  ctx.poll();
  assert(!ctx.link.ready_pending() && ctx.link.ready_progress_ms() == 0 && ctx.self == NodeId::None);
}

static void test_summary_and_preview() {
  const std::string a(32, 'A'), b(32, 'B');
  const auto config = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const auto playing = message("phase", R"({"phase":"Playing"})");
  const auto finished = message("phase", R"({"phase":"Finished"})");
  const auto status = message("status", R"({"score":9,"remaining":0,"ready":false,"countdown":0})");
  const auto summary = [&](const std::string& sync, const std::string& fields = R"("score":2147483647,"rank":2,"eligible":true,"topPercent":null)") {
    return message("summary", "{\"sync\":\"" + sync + "\"," + fields + "}");
  };
  // 同じ遷移を一括・行単位・1バイトずつ受け取っても、終了演出の判定は同じ。
  for (unsigned delivery = 0; delivery < 3; ++delivery) {
    TestContext transition;
    transition.receive(config + reset(a, "[]") + message("phase", R"({"phase":"Preparing"})") + status);
    const std::string events[] = {playing, status, finished, status, summary(a)};
    std::string batch;
    for (const auto& event : events) batch += event;
    if (delivery == 0) transition.receive(batch);
    else if (delivery == 1) for (const auto& event : events) transition.receive(event);
    else for (char byte : batch) transition.receive(std::string(1, byte));
    assert(transition.link.summary().known && transition.link.summary().animate);
    const auto revision = transition.link.summary().revision;
    transition.receive(summary(a));
    assert(transition.link.summary().revision == revision);
    // 再同期では、保持中のPlayingを通常の終了遷移として扱わない。
    transition.receive(playing + message("phase", R"({"phase":"invalid"})"));
    transition.receive(reset(b, "[]") + finished + status + summary(b));
    assert(transition.link.summary().known && !transition.link.summary().animate);
  }
  TestContext ctx;
  ctx.receive(summary(a));
  assert(!ctx.link.summary().known);
  ctx.receive(config + reset(a, "[]") + playing + status);
  ctx.receive(summary(a));
  assert(!ctx.link.summary().known);
  ctx.receive(finished + status + summary(b));
  assert(!ctx.link.summary().known);
  ctx.receive(summary(a));
  assert(ctx.link.summary().known && ctx.link.summary().score == INT32_MAX && ctx.link.summary().rank == 2);
  assert(ctx.link.summary().animate);
  const auto revision = ctx.link.summary().revision;
  ctx.receive(finished + status + summary(a));
  assert(ctx.link.summary().revision == revision && ctx.link.summary().animate);
  ctx.now = 100000;
  ctx.poll();
  assert(ctx.link.fifo_synced()); // 正常な無通信には期限なし。
  ctx.receive(reset(b, "[]") + status);
  assert(!ctx.link.summary().known);
  ctx.receive(summary(a));
  assert(!ctx.link.summary().known);
  ctx.receive(summary(b, R"("score":400,"rank":null,"eligible":false,"topPercent":null)"));
  assert(ctx.link.summary().score == 400 && !ctx.link.summary().eligible && !ctx.link.summary().animate);
  ctx.receive(playing);
  assert(!ctx.link.summary().known);

  // 結果取得期限は通常RESET（異なる世代を含む）・重複PHASE/STATUSでも延ばさない。
  ctx.now = 200000;
  ctx.receive(finished);
  ctx.now += ServerLink::SUMMARY_TIMEOUT_MS - 1;
  ctx.receive(reset(a, "[]") + finished + status + summary(b));
  assert(ctx.link.fifo_synced());
  ++ctx.now;
  ctx.port.output.clear();
  ctx.poll();
  assert(!ctx.link.fifo_synced() && ctx.port.output == message("resync", "{}"));
  ctx.receive(reset(b, "[]") + finished + status + summary(b));
  assert(ctx.link.summary().known && !ctx.link.summary().animate);

  const std::string bad[] = {
    R"("score":-1,"rank":1,"eligible":true,"topPercent":null)",
    R"("score":2147483648,"rank":1,"eligible":true,"topPercent":null)",
    R"("score":true,"rank":1,"eligible":true,"topPercent":null)",
    R"("score":1.5,"rank":1,"eligible":true,"topPercent":null)",
    R"("score":1,"rank":0,"eligible":true,"topPercent":null)", R"("score":1,"rank":9,"eligible":true,"topPercent":null)",
    R"("score":1,"rank":null,"eligible":true,"topPercent":null)",
    R"("score":1,"rank":true,"eligible":true,"topPercent":null)",
    R"("score":1,"rank":1,"eligible":false,"topPercent":null)", R"("score":1,"eligible":false,"topPercent":null)",
    R"("score":1,"rank":null,"topPercent":null)", R"("score":1,"rank":null,"eligible":0,"topPercent":null)",
    R"("rank":1,"eligible":true,"topPercent":null)",
    // topPercentは必須で、1〜50の整数かnull。対象外ではnullだけを受け付ける。
    R"("score":1,"rank":1,"eligible":true)", R"("score":1,"rank":1,"eligible":true,"topPercent":0)",
    R"("score":1,"rank":1,"eligible":true,"topPercent":51)", R"("score":1,"rank":1,"eligible":true,"topPercent":-1)",
    R"("score":1,"rank":1,"eligible":true,"topPercent":12.5)", R"("score":1,"rank":1,"eligible":true,"topPercent":true)",
    R"("score":1,"rank":1,"eligible":true,"topPercent":"12")", R"("score":1,"rank":1,"eligible":true,"topPercent":[12])",
    R"("score":1,"rank":null,"eligible":false,"topPercent":12)", R"("score":1,"rank":null,"eligible":false)"
  };
  for (const unsigned top : {1u, 12u, 50u}) {
    ctx.receive(config + reset(a, "[]") + finished + status +
                summary(a, R"("score":5,"rank":1,"eligible":true,"topPercent":)" + std::to_string(top)));
    assert(ctx.link.fifo_synced() && ctx.link.summary().known && ctx.link.summary().topPercent == top);
  }
  ctx.receive(summary(a, R"("score":5,"rank":1,"eligible":true,"topPercent":51)"));
  assert(!ctx.link.fifo_synced() && ctx.link.summary().topPercent == 50); // 不正な値では正常値を保つ。
  ctx.receive(config + reset(a, "[]") + finished + status);
  ctx.receive(summary(a));
  assert(ctx.link.fifo_synced() && ctx.link.summary().topPercent == 0); // 対象者のnullは上位%なし。
  ctx.receive(summary(a, R"("score":400,"rank":null,"eligible":false,"topPercent":null)"));
  assert(ctx.link.fifo_synced() && !ctx.link.summary().eligible && ctx.link.summary().topPercent == 0);
  for (const auto& fields : bad) {
    ctx.receive(config + reset(a, "[]") + finished + status + summary(a));
    ctx.receive(summary(a, fields));
    assert(!ctx.link.fifo_synced());
    assert(ctx.link.summary().known && ctx.link.summary().score == INT32_MAX && ctx.link.summary().rank == 2);
    assert(!ctx.link.summary().animate);
  }
  for (const auto& sync : {std::string(31, 'A'), std::string(32, 'a'), std::string(32, 'G')}) {
    ctx.receive(config + reset(a, "[]") + finished + status + summary(a));
    ctx.receive(summary(sync));
    assert(!ctx.link.fifo_synced() && ctx.link.summary().score == INT32_MAX);
  }
  ctx.receive(config + reset(a, "[]") + finished + status + summary(a));
  assert(!ctx.link.summary().animate); // 初期同期のFinishedは静止表示。
  ctx.port.connected = false;
  ctx.poll();
  ctx.now += ServerLink::DISCONNECT_GRACE_MS;
  ctx.poll();
  assert(!ctx.link.summary().known && !ctx.link.preview());
  ctx.port.connected = true;

  const auto preview = message("config", R"({"node":8,"mode":"auto","preview":true})");
  ctx.receive(preview + reset(a, R"([{"pid":10,"dst":2}])") + finished + status + summary(a));
  assert(ctx.link.preview() && !ctx.link.summary().known);
  ctx.now += 10000;
  ctx.poll();
  assert(ctx.link.fifo_synced()); // 確認中は結果待ちを開始しない。
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(message("phase", R"({"phase":"Preparing"})"));
  ctx.link.update_ready(ctx.port, 0, ctx.now);
  ctx.link.update_ready(ctx.port, ReadyHold::END_BUTTONS, ++ctx.now);
  ctx.now += ReadyHold::DURATION_MS;
  assert(!ctx.link.update_ready(ctx.port, ReadyHold::END_BUTTONS, ctx.now));
  ctx.receive(config + reset(b, "[]") + finished + status + summary(b));
  assert(!ctx.link.preview() && ctx.link.summary().known && !ctx.link.summary().animate);

  const std::string badConfig[] = {
    "{\"type\":\"config\",\"data\":{\"node\":1,\"mode\":\"manual\"}}\n",
    message("config", R"({"node":1,"mode":"manual","preview":true})"),
    message("config", R"({"node":1,"mode":"auto","preview":0})"),
    message("config", R"({"node":1,"mode":"auto","preview":null})")
  };
  for (const auto& invalid : badConfig) {
    ctx.receive(preview + reset(a, "[]") + finished + status);
    ctx.receive(invalid);
    assert(ctx.self == NodeId::None && !ctx.link.preview() && !ctx.link.summary().known);
    assert(ctx.link.mode() == ControlMode::Auto);
  }
  ctx.receive(preview + reset(a, "[]") + finished + status);
  ctx.receive(message("identify", "{}"));
  assert(!ctx.link.preview() && ctx.self == NodeId::None);
}

static void test_countdown() {
  TestContext ctx;
  const std::string sync = "0000000000000000000000000000000A";
  const std::string fifo = reset(sync, R"([{"pid":10,"dst":2}])");
  const std::string resync = message("resync", "{}");
  const auto countdown = [](const std::string& value) {
    return message("status", R"({"score":0,"remaining":90,"ready":true,"countdown":)" + value + "}");
  };
  ctx.receive(message("config", R"({"node":1,"mode":"manual","preview":false})") + fifo +
              message("phase", R"({"phase":"Playing"})"));
  for (unsigned value : {3u, 3u, 2u, 1u}) {
    ctx.receive(countdown(std::to_string(value))); // 同じ値の再通知も受け付ける。
    assert(ctx.link.status().countdown == value && ctx.link.counting_down() && ctx.link.fifo_synced());
    // FIFOが残っていても、カウントダウン中はボタンのSENDを出さない。
    ctx.port.output.clear();
    assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now) && ctx.port.output.empty());
  }
  ctx.receive(countdown("0"));
  assert(ctx.link.status().countdown == 0 && !ctx.link.counting_down());
  assert(ctx.link.phase() == GamePhase::Playing && ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(result(sync, 0));
  assert(ctx.queue.size() == 0 && !ctx.link.pending());

  const std::string bad[] = {"-1", "4", "255", "256", "1.0", "true", "\"1\"", "null"};
  for (const auto& value : bad) {
    ctx.receive(fifo + countdown("2"));
    ctx.port.output.clear();
    ctx.receive(countdown(value));
    assert(ctx.port.output == resync && !ctx.link.fifo_synced() && ctx.queue.size() == 1);
    assert(ctx.link.status().countdown == 2 && ctx.link.status().score == 0 && ctx.link.status().ready);
  }
  ctx.receive(fifo + countdown("2"));
  ctx.port.output.clear();
  ctx.receive(message("status", R"({"score":0,"remaining":90,"ready":true})")); // 欠落も不正。
  assert(ctx.port.output == resync && !ctx.link.fifo_synced() && ctx.link.status().countdown == 2);

  // 再同期のSTATUS取得待ちでは、保持中の値でSENDを止めない。
  ctx.receive(fifo);
  assert(ctx.link.awaiting_status() && ctx.link.status().countdown == 2 && !ctx.link.counting_down());
  assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(result(sync, 1, false) + countdown("1"));
  assert(!ctx.link.awaiting_status() && ctx.link.counting_down());
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(message("identify", "{}"));
  assert(!ctx.link.status().known && ctx.link.status().countdown == 0 && !ctx.link.counting_down());
}

int main() {
  test_countdown();
  test_summary_and_preview();
  test_ready();
  test_status_and_mode();
  test_utf8_validation();
  test_oversized_line_drop();
  test_unicode_escapes_and_nesting();
  test_partial_writes();
  TestContext ctx;
  const std::string syncA = "0000000000000000000000000000000A";
  const std::string syncB = "0000000000000000000000000000000B";
  const std::string packets = R"([{"pid":10,"dst":2}])";
  const std::string config = message("config", R"({"node":1,"mode":"manual","preview":false})");
  const std::string status = message("status", R"({"score":23,"remaining":87,"ready":true,"countdown":0})");
  const std::string identify = message("identify", "{}");
  const std::string hello = message("hello", R"({"uid":"001122334455"})");
  const std::string resync = message("resync", "{}");
  const std::string preparing = message("phase", R"({"phase":"Preparing"})");
  const std::string playing = message("phase", R"({"phase":"Playing"})");
  const std::string finished = message("phase", R"({"phase":"Finished"})");
  const std::string push = message("push", R"({"pid":11,"dst":3})");
  const std::string resetA = reset(syncA, packets);
  const std::string resetB = reset(syncB, packets);
  uint64_t lastRid = 0;
  bool sent = false;
  auto send = [&](const std::string& sync) {
    ctx.port.output.clear();
    const uint16_t pid = ctx.queue.at(0)->id;
    assert(ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
    JsonDocument request;
    assert(!deserializeJson(request, ctx.port.output));
    assert(request["type"] == "send");
    assert(request["data"]["sync"].as<std::string>() == sync);
    assert(request["data"]["pid"].as<uint16_t>() == pid);
    assert(request["data"]["to"] == 2);
    assert(request["data"]["rid"].is<uint32_t>());
    const uint32_t rid = request["data"]["rid"];
    assert(!sent ? rid == 0 : rid > lastRid);
    sent = true;
    lastRid = rid;
    assert(ctx.link.pending());
    assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
    return rid;
  };

  ctx.poll();
  assert(ctx.port.output == hello && !ctx.link.fifo_synced() && ctx.link.phase() == GamePhase::Unknown);
  ctx.receive(resetA + playing); // 未設定ではFIFOとフェーズを取得しない。
  assert(ctx.queue.size() == 0 && ctx.link.phase() == GamePhase::Unknown);
  for (int i = 0; i < 2; ++i) {
    ctx.port.output.clear();
    ctx.receive(identify);
    assert(ctx.port.output == hello && ctx.self == NodeId::None);
  }
  ctx.receive(config.substr(0, 18));
  assert(ctx.self == NodeId::None);
  ctx.receive(config.substr(18));
  assert(ctx.self == NodeId::P1 && !ctx.link.fifo_synced());
  ctx.receive(push); // 同期待ちではPUSHを適用しない。
  assert(ctx.queue.size() == 0);
  assert(ctx.receive(resetA));
  assert(ctx.link.fifo_synced() && ctx.queue.size() == 1 && ctx.link.phase() == GamePhase::Unknown);
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.now += ServerLink::RETRY_MS;
  ctx.port.output.clear();
  ctx.poll();
  assert(ctx.port.output == resync && !ctx.link.fifo_synced()); // フェーズ未取得でも再試行する。
  ctx.receive(resetA + status);
  ctx.receive(preparing);
  assert(ctx.link.phase() == GamePhase::Preparing && !ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.port.output.clear();
  ctx.now += ServerLink::RETRY_MS;
  ctx.poll();
  assert(ctx.port.output.empty()); // Preparingは正常な無通信状態。

  ctx.receive(playing);
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::None, ctx.now));
  assert(!ctx.link.send(ctx.port, ctx.queue, static_cast<NodeId>(12), ctx.now));
  const uint32_t first = send(syncA);
  ctx.receive(push + result(syncB, first) + result(syncA, first + 1));
  assert(ctx.queue.size() == 2 && ctx.link.pending());
  ctx.receive(finished);
  assert(ctx.link.phase() == GamePhase::Finished && ctx.link.pending());
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(result(syncA, first)); // 終了前の要求に対する確定応答は適用する。
  assert(ctx.queue.size() == 1 && ctx.queue.at(0)->id == 11 && !ctx.link.pending());
  ctx.receive(result(syncA, first));
  assert(ctx.queue.size() == 1 && ctx.link.fifo_synced());
  ctx.receive(playing);
  const uint32_t failed = send(syncA);
  ctx.receive(result(syncA, failed, false));
  assert(ctx.queue.size() == 1 && !ctx.link.pending());
  const uint32_t retry = send(syncA);
  ctx.receive(result(syncA, failed)); // 同じ世代でも旧RIDは無視する。
  assert(ctx.link.pending() && ctx.queue.size() == 1);
  ctx.receive(result(syncA, retry));
  assert(ctx.queue.size() == 0 && !ctx.link.pending());

  ctx.receive(resetA);
  const uint32_t old = send(syncA);
  ctx.receive(resetB);
  assert(!ctx.link.pending() && ctx.link.phase() == GamePhase::Playing);
  const uint32_t current = send(syncB);
  ctx.receive(result(syncA, old));
  assert(ctx.queue.size() == 1 && ctx.link.pending());
  ctx.receive(result(syncB, current));
  assert(ctx.queue.size() == 0 && !ctx.link.pending());

  ctx.receive(resetA);
  send(syncA);
  ctx.receive(preparing + reset(syncB, "[]")); // ゲームのResetはphase→reset。
  assert(ctx.link.phase() == GamePhase::Preparing && ctx.queue.size() == 0 && !ctx.link.pending());
  ctx.receive(resetA + playing);
  const uint32_t beforeConfig = send(syncA);
  ctx.receive(config);
  assert(!ctx.link.fifo_synced() && !ctx.link.pending() && ctx.link.phase() == GamePhase::Unknown);
  ctx.receive(playing + result(syncA, beforeConfig) + push); // phaseは同期待ちでも保存する。
  assert(ctx.link.phase() == GamePhase::Playing && ctx.queue.size() == 0);
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(resetB + status);
  const uint32_t afterConfig = send(syncB);
  ctx.receive(result(syncA, beforeConfig));
  assert(ctx.link.pending());
  ctx.receive(result(syncB, afterConfig));

  ctx.receive(resetA);
  const uint32_t timeout = send(syncA);
  ctx.now += ServerLink::SEND_TIMEOUT_MS - 1;
  ctx.receive(result(syncB, timeout) + result(syncA, timeout + 1));
  assert(ctx.link.pending());
  ++ctx.now;
  ctx.port.output.clear();
  ctx.poll();
  assert(!ctx.link.fifo_synced() && !ctx.link.pending() && ctx.queue.size() == 1 && ctx.port.output == resync);
  ctx.port.output.clear();
  ctx.receive(result(syncA, timeout)); // 同期待ちの遅延応答は再試行を即時発生させない。
  assert(ctx.queue.size() == 1 && ctx.port.output.empty());
  ctx.receive(resetB);
  const uint32_t mismatch = send(syncB);
  ctx.queue.pop(); // 正本との不一致を模擬。
  ctx.receive(result(syncB, mismatch));
  assert(!ctx.link.fifo_synced() && !ctx.link.pending());
  ctx.receive(resetA);

  ctx.receive(R"({"type":"push")");
  ctx.now += ServerLink::RECEIVE_TIMEOUT_MS;
  ctx.port.output.clear();
  ctx.poll();
  assert(!ctx.link.fifo_synced() && ctx.queue.size() == 0 && ctx.port.output == hello);
  ctx.receive(config + resetA + playing + status);

  const std::string invalid[] = {
    "[]\n", message("hello", "{}"), message("unknown", "{}"),
    message("phase", "{}"), message("phase", R"({"phase":"playing"})"),
    message("phase", R"({"phase":1})"), message("phase", R"({"phase":"Playing\u0000x"})"),
    message("push", R"({"pid":-1,"dst":2})"), message("push", R"({"pid":65536,"dst":2})"),
    message("push", R"({"pid":"12","dst":2})"), message("push", R"({"pid":12,"dst":12})"),
    message("push", R"({"pid":10,"dst":2})"),
    message("result", R"({"ok":true})"),
    message("result", "{\"sync\":\"" + syncA + "\",\"rid\":0,\"ok\":1}"),
    message("result", "{\"sync\":\"" + syncA + "\",\"rid\":-1,\"ok\":true}"),
    message("result", "{\"sync\":\"" + syncA + "\",\"rid\":4294967296,\"ok\":true}"),
    message("result", "{\"sync\":\"" + syncA + "\",\"rid\":1.0,\"ok\":true}"),
    message("result", "{\"sync\":\"" + syncA + "\",\"rid\":true,\"ok\":true}"),
    reset("0000000000000000000000000000000a", packets), reset("A", packets),
    message("reset", R"({"packets":[]})"),
    reset(syncA, R"([{"pid":20,"dst":2},{}])"),
    reset(syncA, R"([{"pid":20,"dst":2},{"pid":20,"dst":3}])")
  };
  for (const auto& bytes : invalid) {
    ctx.port.output.clear();
    ctx.receive(bytes);
    assert(!ctx.link.fifo_synced() && ctx.queue.size() == 1 && ctx.queue.at(0)->id == 10);
    assert(ctx.port.output == resync);
    assert(ctx.receive(resetA) && ctx.link.fifo_synced());
  }

  std::string full = "[";
  for (size_t i = 0; i < PacketQueue::capacity(); ++i) {
    if (i) full += ',';
    full += R"({"pid":)" + std::to_string(i) + R"(,"dst":8})";
  }
  full += "]";
  assert(ctx.receive(reset(syncB, full)));
  assert(ctx.link.fifo_synced() && ctx.queue.size() == PacketQueue::capacity() && ctx.queue.at(1023)->id == 1023);
  ctx.receive(message("push", R"({"pid":2000,"dst":1})"));
  assert(!ctx.link.fifo_synced() && ctx.queue.size() == PacketQueue::capacity());
  full.insert(full.size() - 1, R"(,{"pid":2000,"dst":8})");
  assert(!ctx.receive(reset(syncA, full)) && ctx.queue.size() == PacketQueue::capacity());
  ctx.receive(resetA);
  // 上限ちょうどのJSON行、CRLF、空行、追加フィールド。
  std::string boundary = message("phase", R"({"phase":"Playing","extra":0})");
  boundary.pop_back();
  boundary.insert(boundary.size() - 1, ServerLink::MAX_LINE_BYTES - boundary.size(), ' ');
  ctx.receive(boundary + "\r\n\r\n\n");
  assert(ctx.link.fifo_synced() && ctx.link.phase() == GamePhase::Playing);

  ctx.port.writable = 0;
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now) && !ctx.link.pending());
  ctx.port.writable = 256;
  const uint32_t beforeIdentify = send(syncA);
  ctx.port.output.clear();
  ctx.receive(identify);
  assert(ctx.port.output == hello && ctx.self == NodeId::None && !ctx.link.pending());
  assert(ctx.link.phase() == GamePhase::Unknown && !ctx.link.fifo_synced());
  ctx.receive(result(syncA, beforeIdentify) + resetA + finished);
  assert(ctx.queue.size() == 0 && ctx.link.phase() == GamePhase::Unknown);
  ctx.receive(config + resetB + playing + status);
  const uint32_t afterIdentify = send(syncB);
  ctx.receive(result(syncB, afterIdentify));
  ctx.receive(resetA);

  ctx.port.connected = false;
  ctx.poll();
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.now += ServerLink::DISCONNECT_GRACE_MS - 1;
  ctx.port.connected = true;
  ctx.poll();
  assert(ctx.link.fifo_synced() && ctx.link.phase() == GamePhase::Playing);
  const uint32_t beforeDisconnect = send(syncA);
  ctx.port.connected = false;
  ctx.poll();
  ctx.now += ServerLink::DISCONNECT_GRACE_MS;
  ctx.poll();
  assert(!ctx.link.fifo_synced() && ctx.self == NodeId::None && !ctx.link.pending() && ctx.link.phase() == GamePhase::Unknown);
  ctx.port.connected = true;
  ctx.port.output.clear();
  ctx.poll();
  assert(ctx.port.output == hello);
  ctx.receive(config + resetB + finished + status + result(syncA, beforeDisconnect));
  assert(ctx.link.fifo_synced() && ctx.queue.size() == 1 && ctx.link.phase() == GamePhase::Finished);
  assert(!ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  ctx.receive(playing);
  const uint32_t afterDisconnect = send(syncB);
  ctx.receive(result(syncB, afterDisconnect));

  // millis()の周回をまたいでも、応答タイムアウトを判定する。
  ctx.receive(resetA);
  ctx.now = UINT32_MAX - 500;
  send(syncA);
  ctx.now += ServerLink::SEND_TIMEOUT_MS;
  ctx.poll();
  assert(!ctx.link.fifo_synced() && !ctx.link.pending());
  ctx.receive(reset(syncB, "[]"));
  assert(ctx.link.fifo_synced() && !ctx.link.send(ctx.port, ctx.queue, NodeId::P2, ctx.now));
  std::cout << "PASS: identify, sync/rid, phase gating, countdown, delayed results, reset/config/reconnect, "
               "send/receive timeouts, partial writes, strict JSON/UTF-8, oversized-line discard, "
               "validation, line boundaries, capacity\n";
}
