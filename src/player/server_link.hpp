#ifndef SERVER_LINK_HPP_
#define SERVER_LINK_HPP_

#include <ArduinoJson.h>
#include <algorithm>
#include <cstdio>
#include <cstring>
#include "packet.hpp"

// PacketQueueと同じくゲーム側のloopだけから呼び出す。
class ServerLink {
public:
  static constexpr size_t MAX_LINE_BYTES = 32768;
  static constexpr uint32_t RETRY_MS = 1000;
  static constexpr uint32_t SEND_TIMEOUT_MS = 3000;
  static constexpr uint32_t RECEIVE_TIMEOUT_MS = 1000;
  static constexpr uint32_t DISCONNECT_GRACE_MS = 100;

  // 戻り値はRESETの適用有無。描画アニメーションも同時に初期化する。
  template<class Port>
  bool poll(Port& port, const char* uid, PacketQueue& queue, NodeId& selfId, uint32_t nowMs) {
    if (!port) {
      if (!disconnectSeen_) {
        disconnectSeen_ = true;
        disconnectedMs_ = nowMs;
      }
      // Hardware CDCの接続判定は短時間falseになることがある。
      if (nowMs - disconnectedMs_ >= DISCONNECT_GRACE_MS) {
        state_ = State::AwaitConfig;
        pending_ = false;
        resyncNeeded_ = true;
        length_ = 0;
        dropping_ = false;
      }
      return false;
    }
    disconnectSeen_ = false;

    bool reset = false;
    // 大きなRESETを受信中もボタンと描画スナップショットの更新へ戻る。
    for (size_t budget = 4096; budget && port.available(); --budget) {
      const int value = port.read();
      if (value < 0) break;
      receivedMs_ = nowMs;
      if (value == '\n') {
        if (dropping_) {
          request_resync();
        } else if (length_) {
          reset |= receive(queue, selfId);
        }
        length_ = 0;
        dropping_ = false;
      } else if (!dropping_) {
        if (length_ == MAX_LINE_BYTES) {
          dropping_ = true;
        } else {
          line_[length_++] = static_cast<char>(value);
        }
      }
    }

    if ((length_ || dropping_) && nowMs - receivedMs_ >= RECEIVE_TIMEOUT_MS) {
      length_ = 0;
      dropping_ = false;
      request_resync();
    }
    if (pending_ && nowMs - sentMs_ >= SEND_TIMEOUT_MS) request_resync();
    if (!ready() && (resyncNeeded_ || nowMs - handshakeMs_ >= RETRY_MS)) {
      char message[96];
      if (state_ == State::AwaitConfig) {
        std::snprintf(message, sizeof(message), "{\"type\":\"hello\",\"data\":{\"uid\":\"%s\"}}\n", uid);
      } else {
        std::snprintf(message, sizeof(message), "{\"type\":\"resync\",\"data\":{}}\n");
      }
      if (write_line(port, message)) {
        handshakeMs_ = nowMs;
        resyncNeeded_ = false;
      }
    }
    return reset;
  }

  template<class Port>
  bool send(Port& port, const PacketQueue& queue, NodeId to, uint32_t nowMs) {
    if (!ready() || pending_ || !queue.size()) return false;
    const unsigned target = static_cast<unsigned>(to);
    if (target < 1 || target > static_cast<unsigned>(NodeId::B3)) return false;
    char message[96];
    std::snprintf(message, sizeof(message), "{\"type\":\"send\",\"data\":{\"pid\":%u,\"to\":%u}}\n",
                  static_cast<unsigned>(queue.at(0)->id), target);
    if (!write_line(port, message)) return false;
    pending_ = true;
    pendingPid_ = queue.at(0)->id;
    sentMs_ = nowMs;
    return true;
  }

  bool ready() const { return state_ == State::Ready; }
  bool pending() const { return pending_; }

private:
  enum class State : uint8_t { AwaitConfig, AwaitReset, Ready };

  template<class Port>
  static bool write_line(Port& port, const char* message) {
    const size_t size = std::strlen(message);
    if (port.availableForWrite() < static_cast<int>(size)) return false;
    return port.write(reinterpret_cast<const uint8_t*>(message), size) == size;
  }

  void request_resync() {
    if (state_ != State::AwaitConfig) state_ = State::AwaitReset;
    pending_ = false;
    resyncNeeded_ = true;
  }

  static bool valid_packet(JsonVariantConst value) {
    return value.is<JsonObjectConst>() && value["pid"].is<uint16_t>() &&
           value["dst"].is<uint8_t>() && value["dst"].as<unsigned>() >= 1 &&
           value["dst"].as<unsigned>() <= static_cast<unsigned>(NodeId::B3);
  }

  static Packet packet_from(JsonVariantConst value) {
    return {value["pid"].as<uint16_t>(), static_cast<NodeId>(value["dst"].as<uint8_t>())};
  }

  static bool apply_reset(JsonArrayConst packets, PacketQueue& queue) {
    if (packets.isNull() || packets.size() > PacketQueue::capacity()) return false;

    // 先に全要素を検証し、不正なRESETで既存キャッシュを失わないようにする。
    std::array<uint16_t, PacketQueue::capacity()> ids;
    size_t count = 0;
    for (JsonVariantConst packet : packets) {
      if (!valid_packet(packet)) return false;
      ids[count++] = packet["pid"].as<uint16_t>();
    }
    std::sort(ids.begin(), ids.begin() + count);
    if (std::adjacent_find(ids.begin(), ids.begin() + count) != ids.begin() + count) return false;

    queue.clear();
    for (JsonVariantConst value : packets) queue.push(packet_from(value));
    return true;
  }

  bool receive(PacketQueue& queue, NodeId& selfId) {
    JsonDocument message;
    const auto error = deserializeJson(message, line_, length_, DeserializationOption::NestingLimit(4));
    if (error || !message.is<JsonObject>() || !message["type"].is<const char*>() ||
        !message["data"].is<JsonObject>()) {
      request_resync();
      return false;
    }
    const char* type = message["type"];
    const JsonObjectConst data = message["data"];

    if (std::strcmp(type, "config") == 0) {
      if (!data["node"].is<uint8_t>() || data["node"].as<unsigned>() < 1 ||
          data["node"].as<unsigned>() > 8) {
        request_resync();
        return false;
      }
      selfId = static_cast<NodeId>(data["node"].as<uint8_t>());
      state_ = State::AwaitReset;
      request_resync();
    } else if (std::strcmp(type, "reset") == 0 && state_ != State::AwaitConfig) {
      if (!apply_reset(data["packets"].as<JsonArrayConst>(), queue)) {
        request_resync();
        return false;
      }
      state_ = State::Ready;
      pending_ = resyncNeeded_ = false;
      return true;
    } else if (std::strcmp(type, "push") == 0 && ready()) {
      if (!valid_packet(data)) {
        request_resync();
        return false;
      }
      const Packet packet = packet_from(data);
      for (size_t i = 0; i < queue.size(); ++i) {
        if (queue.at(i)->id == packet.id) {
          request_resync();
          return false;
        }
      }
      if (!queue.push(packet)) request_resync();
    } else if (std::strcmp(type, "result") == 0 && ready()) {
      if (!data["ok"].is<bool>()) {
        request_resync();
      } else if (pending_) {
        if (!queue.size() || queue.at(0)->id != pendingPid_) {
          request_resync();
        } else {
          if (data["ok"].as<bool>()) queue.pop();
          pending_ = false;
        }
      }
    } else {
      request_resync();
    }
    return false;
  }

  char line_[MAX_LINE_BYTES]{};
  size_t length_ = 0;
  bool dropping_ = false;
  State state_ = State::AwaitConfig;
  bool pending_ = false;
  bool resyncNeeded_ = true;
  bool disconnectSeen_ = false;
  uint16_t pendingPid_ = 0;
  uint32_t sentMs_ = 0;
  uint32_t handshakeMs_ = 0;
  uint32_t receivedMs_ = 0;
  uint32_t disconnectedMs_ = 0;
};

#endif // SERVER_LINK_HPP_
