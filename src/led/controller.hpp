#ifndef LED_CONTROLLER_HPP_
#define LED_CONTROLLER_HPP_

#include <ArduinoJson.h>
#include <array>
#include <cstdio>
#include <cstring>
#include "node_config.hpp"

namespace led {

constexpr uint32_t FRAME_MS = 10;
constexpr uint8_t TAIL[] = {255, 128, 64, 32, 16, 8, 4, 2, 1, 0};

inline Color pulse_pixel(Color color, bool incoming, uint16_t count,
                         uint16_t pixel, uint32_t ageMs) {
  if (pixel >= count) return {};
  const uint32_t distance = incoming ? count - 1 - pixel : pixel;
  const uint32_t frame = ageMs / FRAME_MS;
  if (frame < distance || frame - distance >= sizeof(TAIL)) return {};
  const unsigned intensity = TAIL[frame - distance];
  return {static_cast<uint8_t>((color.r * intensity + 127) / 255),
          static_cast<uint8_t>((color.g * intensity + 127) / 255),
          static_cast<uint8_t>((color.b * intensity + 127) / 255)};
}

class Controller {
public:
  static constexpr size_t MAX_STRIPS = 7;
  static constexpr size_t MAX_LIGHTS = 128;
  static constexpr size_t MAX_LINE_BYTES = 96;
  static constexpr uint32_t RECEIVE_TIMEOUT_MS = 1000;
  static constexpr uint32_t RETRY_MS = 1000;
  static constexpr uint32_t DISCONNECT_GRACE_MS = 100;
  static constexpr uint32_t PREVIEW_PERIOD_MS = 1000;

  // CONFIG適用または設定の無効化で出力の再設定が必要な場合にtrueを返す。
  template<class Port>
  bool poll(Port& port, const char* uid, uint32_t nowMs) {
    return poll(port, uid, nowMs, [] {});
  }

  // 出力の初期化・消灯は、応答送信と次の命令の処理より前に完了させる。
  template<class Port, class ApplyOutputs>
  bool poll(Port& port, const char* uid, uint32_t nowMs, ApplyOutputs applyOutputs) {
    update(nowMs);
    if (!port) {
      if (!disconnectSeen_) {
        disconnectSeen_ = true;
        disconnectedMs_ = nowMs;
      }
      if (nowMs - disconnectedMs_ >= DISCONNECT_GRACE_MS) {
        const bool changed = ready();
        reset();
        if (changed) applyOutputs();
        // CDC側に残った旧接続の入力も、切断確定中は破棄する。
        for (int remaining = port.available(); remaining > 0; --remaining) {
          if (port.read() < 0) break;
        }
        return changed;
      }
      return false;
    }
    disconnectSeen_ = false;
    // 起動・再接続のHELLOが送れるまでは、新しい命令を適用しない。
    if (greetingPending_) {
      send_hello(port, uid, nowMs);
      if (!helloSent_) return false;
      greetingPending_ = false;
    }
    // 途切れた行は次のLFまで捨て、後半だけを命令として解釈しない。
    bool configured = false;
    auto apply = [&] {
      configured = true;
      applyOutputs();
    };
    auto recover = [&] {
      if (unconfigure()) {
        ++pendingHellos_;
        apply();
      }
      send_hello(port, uid, nowMs);
    };
    if (!dropping_ && (length_ || pendingCr_) &&
        nowMs - receivedMs_ >= RECEIVE_TIMEOUT_MS) {
      length_ = 0;
      pendingCr_ = false;
      dropping_ = true;
      recover();
    }

    for (size_t budget = 1024; budget && port.available(); --budget) {
      const int value = port.read();
      if (value < 0) break;
      receivedMs_ = nowMs;
      if (value == '\n') {
        if (!dropping_ && length_) {
          line_[length_] = '\0';
          const Command command = receive(nowMs);
          if (command == Command::Configured) {
            apply();
          } else if (command == Command::Identify) {
            const bool changed = unconfigure();
            ++pendingHellos_;
            if (changed) apply();
          } else if (command == Command::Recover) {
            recover();
          }
          send_hello(port, uid, nowMs);
        }
        length_ = 0;
        pendingCr_ = dropping_ = false;
      } else if (!dropping_) {
        if (!pendingCr_ && value == '\r') {
          pendingCr_ = true;
        } else if (pendingCr_ || length_ == MAX_LINE_BYTES || value > 126 ||
                   (value < 32 && value != '\t')) {
          length_ = 0;
          pendingCr_ = false;
          dropping_ = true;
          recover();
        } else {
          line_[length_++] = static_cast<char>(value);
        }
      }
    }

    send_hello(port, uid, nowMs);
    return configured;
  }

  void reset() {
    unconfigure();
    length_ = 0;
    pendingCr_ = dropping_ = helloSent_ = false;
    pendingHellos_ = 0;
    greetingPending_ = true;
  }

  // 同じ接続での出力初期化失敗では、受信行と未送信応答を保持する。
  void output_failed() {
    unconfigure();
    if (!pendingHellos_) pendingHellos_ = 1;
  }

  bool ready() const { return node_ != nullptr; }
  size_t strip_count() const { return stripCount_; }
  const Route& route(size_t strip) const { return *routes_[strip]; }

  Color pixel(size_t strip, uint16_t index, uint32_t nowMs) const {
    const Route& target = route(strip);
    if (index >= target.count) return {};
    if (preview_) {
      const uint32_t elapsed = nowMs - previewStartedMs_;
      const uint32_t phase = elapsed % PREVIEW_PERIOD_MS;
      if (phase >= PREVIEW_PERIOD_MS / 2 ||
          elapsed / PREVIEW_PERIOD_MS == previewClearedCycle_ || target.pin > 1) return {};
      if (std::strcmp(target.node, target.neighbor) == 0) return {255, 0, 0};
      return pulse_pixel(node_color(node_id(node_)), false, target.count, index, phase);
    }
    if (std::strcmp(target.node, target.neighbor) == 0) return tower_;
    Color result;
    for (size_t i = 0; i < lightCount_; ++i) {
      const Light& light = lights_[i];
      if (light.strip == strip) {
        result.add(pulse_pixel(node_color(light.destination), light.incoming,
                               target.count, index, nowMs - light.startedMs));
      }
    }
    return result;
  }

private:
  enum class Command { Ignored, Configured, Identify, Recover };

  template<class Port>
  void send_hello(Port& port, const char* uid, uint32_t nowMs) {
    if (!pendingHellos_ && (ready() || (helloSent_ && nowMs - helloMs_ < RETRY_MS))) return;
    char message[64];
    const int size = std::snprintf(message, sizeof(message), "HELLO %s\n", uid);
    if (size <= 0 || size >= static_cast<int>(sizeof(message))) return;
    do {
      if (port.availableForWrite() < size ||
          port.write(reinterpret_cast<const uint8_t*>(message), size) != static_cast<size_t>(size)) return;
      if (pendingHellos_) --pendingHellos_;
      helloSent_ = true;
      helloMs_ = nowMs;
    } while (pendingHellos_);
  }

  // 受信途中の異常では、次のLFまでの行破棄状態を維持する。
  bool unconfigure() {
    const bool changed = ready();
    stripCount_ = lightCount_ = 0;
    node_ = nullptr;
    tower_ = {};
    preview_ = false;
    previewStartedMs_ = 0;
    previewClearedCycle_ = UINT32_MAX;
    if (changed) helloSent_ = false;
    return changed;
  }

  struct JsonInput {
    const char* next;
    int read() { return *next ? static_cast<unsigned char>(*next++) : -1; }
  };

  // ArduinoJsonが許容する単一引用符・裸のキー・JSON外の数値表記を拒否する。
  // 字句だけを検証し、オブジェクトや配列の構造はArduinoJsonに任せる。
  static bool json_tokens(const char* text) {
    auto digit = [](char c) { return c >= '0' && c <= '9'; };
    bool string = false;
    while (*text) {
      if (*text == ' ' || *text == '\t') {
        ++text;
        continue;
      }
      if (*text == '"') {
        ++text;
        while (*text && *text != '"') {
          if (*text < 32) return false;
          if (*text++ != '\\') continue;
          const char escape = *text++;
          if (!escape) return false;
          if (escape == 'u') {
            for (unsigned i = 0; i < 4; ++i) {
              const char c = *text;
              if (!digit(c) && !(c >= 'a' && c <= 'f') && !(c >= 'A' && c <= 'F')) return false;
              ++text;
            }
          } else if (!std::strchr("\"\\/bfnrt", escape)) {
            return false;
          }
        }
        if (*text++ != '"') return false;
        string = true;
        continue;
      }
      if (*text == ':' && !string) return false;
      string = false;
      if (std::strchr("{}[],:", *text)) {
        ++text;
        continue;
      }
      if (std::strncmp(text, "true", 4) == 0 || std::strncmp(text, "null", 4) == 0) {
        text += 4;
      } else if (std::strncmp(text, "false", 5) == 0) {
        text += 5;
      } else {
        if (*text == '-') ++text;
        if (*text == '0') ++text;
        else {
          if (*text < '1' || *text > '9') return false;
          while (digit(*text)) ++text;
        }
        if (*text == '.') {
          ++text;
          if (!digit(*text)) return false;
          while (digit(*text)) ++text;
        }
        if (*text == 'e' || *text == 'E') {
          ++text;
          if (*text == '+' || *text == '-') ++text;
          if (!digit(*text)) return false;
          while (digit(*text)) ++text;
        }
      }
      if (*text && !std::strchr(" \t,]}", *text)) return false;
    }
    return true;
  }

  bool identify(const char* text) const {
    if (!json_tokens(text)) return false;
    JsonDocument message;
    JsonInput input{text};
    const auto error = deserializeJson(message, input,
        DeserializationOption::NestingLimit(MAX_LINE_BYTES));
    if (error || !message.is<JsonObject>() || !message["type"].is<const char*>() ||
        !message["data"].is<JsonObject>()) return false;
    // ArduinoJsonはオブジェクト終端で停止するため、残りも検証する。
    while (*input.next == ' ' || *input.next == '\t') ++input.next;
    const JsonString type = message["type"].as<JsonString>();
    return !*input.next && type.size() == 8 && std::memcmp(type.c_str(), "identify", 8) == 0;
  }

  struct Light {
    uint32_t startedMs;
    size_t strip;
    unsigned destination;
    bool incoming;
  };

  void update(uint32_t nowMs) {
    for (size_t i = 0; i < lightCount_;) {
      if ((nowMs - lights_[i].startedMs) / FRAME_MS >=
          route(lights_[i].strip).count + sizeof(TAIL) - 1) {
        lights_[i] = lights_[--lightCount_];
      } else {
        ++i;
      }
    }
  }

  Command receive(uint32_t nowMs) {
    const char* text = line_;
    while (*text == ' ' || *text == '\t') ++text;
    if (*text == '{') return identify(text) ? Command::Identify : Command::Recover;
    char* args[5];
    size_t count = 0;
    char* save = nullptr;
    for (char* token = strtok_r(line_, " \t", &save); token && count < 5;
         token = strtok_r(nullptr, " \t", &save)) {
      args[count++] = token;
    }
    if (!count) return Command::Ignored;
    if (std::strcmp(args[0], "CONFIG") == 0) {
      if (count != 3 || (std::strcmp(args[2], "true") != 0 &&
                         std::strcmp(args[2], "false") != 0)) return Command::Recover;
      bool found = false;
      for (const Route& route : ROUTES) found |= std::strcmp(route.node, args[1]) == 0;
      if (!found) return Command::Recover;
      stripCount_ = lightCount_ = 0;
      tower_ = {};
      for (const Route& route : ROUTES) {
        if (std::strcmp(route.node, args[1]) == 0) routes_[stripCount_++] = &route;
      }
      node_ = routes_[0]->node;
      preview_ = std::strcmp(args[2], "true") == 0;
      previewStartedMs_ = nowMs;
      previewClearedCycle_ = UINT32_MAX;
      return Command::Configured;
    }
    if (std::strcmp(args[0], "LIGHT") != 0 && std::strcmp(args[0], "BUFFER") != 0 &&
        std::strcmp(args[0], "CLEAR") != 0 && std::strcmp(args[0], "HELLO") != 0) {
      return Command::Recover;
    }
    if (!ready()) return Command::Ignored;
    if (std::strcmp(args[0], "CLEAR") == 0 && count == 1) {
      lightCount_ = 0;
      tower_ = {};
      if (preview_) previewClearedCycle_ = (nowMs - previewStartedMs_) / PREVIEW_PERIOD_MS;
    } else if (std::strcmp(args[0], "BUFFER") == 0 && count == 2 && node_[0] == 'B') {
      if (std::strlen(args[1]) != 1 || args[1][0] < '0' || args[1][0] > '4') return Command::Ignored;
      const unsigned used = args[1][0] - '0';
      if (preview_) return Command::Ignored;
      tower_ = used <= 1 ? Color{0, 0, 255} : used == 2 ? Color{0, 255, 0} :
               used == 3 ? Color{255, 255, 0} : Color{255, 0, 0};
    } else if (std::strcmp(args[0], "LIGHT") == 0 && count == 4) {
      const unsigned destination = node_id(args[2]);
      const bool incoming = std::strcmp(args[3], "incoming") == 0;
      if (!destination || (!incoming && std::strcmp(args[3], "outgoing") != 0)) return Command::Ignored;
      for (size_t strip = 0; strip < stripCount_; ++strip) {
        if (std::strcmp(route(strip).neighbor, args[1]) != 0 ||
            std::strcmp(node_, args[1]) == 0) continue;
        // ponytail: 同時128本まで。上限を増やす場合はRAMと描画時間を再評価する。
        if (!preview_ && lightCount_ < MAX_LIGHTS) lights_[lightCount_++] = {nowMs, strip, destination, incoming};
        break;
      }
    }
    return Command::Ignored;
  }

  const char* node_ = nullptr;
  std::array<const Route*, MAX_STRIPS> routes_{};
  size_t stripCount_ = 0;
  std::array<Light, MAX_LIGHTS> lights_{};
  size_t lightCount_ = 0;
  Color tower_{};
  bool preview_ = false;
  uint32_t previewStartedMs_ = 0;
  uint32_t previewClearedCycle_ = UINT32_MAX;
  char line_[MAX_LINE_BYTES + 1]{};
  size_t length_ = 0;
  bool pendingCr_ = false;
  bool dropping_ = false;
  uint32_t receivedMs_ = 0;
  bool helloSent_ = false;
  bool greetingPending_ = true;
  size_t pendingHellos_ = 0;
  uint32_t helloMs_ = 0;
  bool disconnectSeen_ = false;
  uint32_t disconnectedMs_ = 0;
};

} // namespace led

#endif // LED_CONTROLLER_HPP_
