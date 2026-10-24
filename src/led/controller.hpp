#ifndef LED_CONTROLLER_HPP_
#define LED_CONTROLLER_HPP_

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

  // CONFIG適用または切断でGPIO設定が変わった場合にtrueを返す。
  template<class Port>
  bool poll(Port& port, const char* uid, uint32_t nowMs) {
    update(nowMs);
    if (!port) {
      if (!disconnectSeen_) {
        disconnectSeen_ = true;
        disconnectedMs_ = nowMs;
      }
      if (nowMs - disconnectedMs_ >= DISCONNECT_GRACE_MS) {
        const bool changed = ready();
        reset();
        return changed;
      }
      return false;
    }
    disconnectSeen_ = false;
    // 途切れた行は次のLFまで捨て、後半だけを命令として解釈しない。
    if ((length_ || dropping_) && nowMs - receivedMs_ >= RECEIVE_TIMEOUT_MS) {
      length_ = 0;
      dropping_ = true;
    }

    bool configured = false;
    for (size_t budget = 1024; budget && port.available(); --budget) {
      const int value = port.read();
      if (value < 0) break;
      receivedMs_ = nowMs;
      if (value == '\n') {
        if (!dropping_ && length_) {
          line_[length_] = '\0';
          configured |= receive(nowMs);
        }
        length_ = 0;
        dropping_ = false;
      } else if (!dropping_) {
        if (length_ == MAX_LINE_BYTES || value == 0 || value > 127) {
          dropping_ = true;
        } else {
          line_[length_++] = static_cast<char>(value);
        }
      }
    }

    if (!ready() && (!helloSent_ || nowMs - helloMs_ >= RETRY_MS)) {
      char message[64];
      const int size = std::snprintf(message, sizeof(message), "HELLO %s\n", uid);
      if (size > 0 && size < static_cast<int>(sizeof(message)) &&
          port.availableForWrite() >= size &&
          port.write(reinterpret_cast<const uint8_t*>(message), size) == static_cast<size_t>(size)) {
        helloSent_ = true;
        helloMs_ = nowMs;
      }
    }
    return configured;
  }

  void reset() {
    stripCount_ = lightCount_ = 0;
    node_ = nullptr;
    tower_ = {};
    length_ = 0;
    dropping_ = helloSent_ = false;
  }

  bool ready() const { return node_ != nullptr; }
  size_t strip_count() const { return stripCount_; }
  const Route& route(size_t strip) const { return *routes_[strip]; }

  Color pixel(size_t strip, uint16_t index, uint32_t nowMs) const {
    const Route& target = route(strip);
    if (index >= target.count) return {};
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
  struct Light {
    uint32_t startedMs;
    size_t strip;
    unsigned destination;
    bool incoming;
  };

  void update(uint32_t nowMs) {
    for (size_t i = 0; i < lightCount_;) {
      if ((nowMs - lights_[i].startedMs) / FRAME_MS >=
          route(lights_[i].strip).count + sizeof(TAIL)) {
        lights_[i] = lights_[--lightCount_];
      } else {
        ++i;
      }
    }
  }

  bool receive(uint32_t nowMs) {
    char* args[5];
    size_t count = 0;
    char* save = nullptr;
    for (char* token = strtok_r(line_, " \t\r", &save); token;
         token = strtok_r(nullptr, " \t\r", &save)) {
      if (count == 5) return false;
      args[count++] = token;
    }
    if (!count) return false;
    if (std::strcmp(args[0], "CONFIG") == 0 && count == 2) {
      bool found = false;
      for (const Route& route : ROUTES) found |= std::strcmp(route.node, args[1]) == 0;
      if (!found) return false;
      stripCount_ = lightCount_ = 0;
      tower_ = {};
      for (const Route& route : ROUTES) {
        if (std::strcmp(route.node, args[1]) == 0) routes_[stripCount_++] = &route;
      }
      node_ = routes_[0]->node;
      return true;
    }
    if (!ready()) return false;
    if (std::strcmp(args[0], "CLEAR") == 0 && count == 1) {
      lightCount_ = 0;
      tower_ = {};
    } else if (std::strcmp(args[0], "BUFFER") == 0 && count == 2 && node_[0] == 'B') {
      if (std::strlen(args[1]) != 1 || args[1][0] < '0' || args[1][0] > '4') return false;
      const unsigned used = args[1][0] - '0';
      tower_ = used <= 1 ? Color{0, 0, 255} : used == 2 ? Color{0, 255, 0} :
               used == 3 ? Color{255, 255, 0} : Color{255, 0, 0};
    } else if (std::strcmp(args[0], "LIGHT") == 0 && count == 4) {
      const unsigned destination = node_id(args[2]);
      const bool incoming = std::strcmp(args[3], "incoming") == 0;
      if (!destination || (!incoming && std::strcmp(args[3], "outgoing") != 0)) return false;
      for (size_t strip = 0; strip < stripCount_; ++strip) {
        if (std::strcmp(route(strip).neighbor, args[1]) != 0 ||
            std::strcmp(node_, args[1]) == 0) continue;
        // ponytail: 同時128本まで。上限を増やす場合はRAMと描画時間を再評価する。
        if (lightCount_ < MAX_LIGHTS) lights_[lightCount_++] = {nowMs, strip, destination, incoming};
        break;
      }
    }
    return false;
  }

  const char* node_ = nullptr;
  std::array<const Route*, MAX_STRIPS> routes_{};
  size_t stripCount_ = 0;
  std::array<Light, MAX_LIGHTS> lights_{};
  size_t lightCount_ = 0;
  Color tower_{};
  char line_[MAX_LINE_BYTES + 1]{};
  size_t length_ = 0;
  bool dropping_ = false;
  uint32_t receivedMs_ = 0;
  bool helloSent_ = false;
  uint32_t helloMs_ = 0;
  bool disconnectSeen_ = false;
  uint32_t disconnectedMs_ = 0;
};

} // namespace led

#endif // LED_CONTROLLER_HPP_
