#ifndef PACKET_DEMO_HPP_
#define PACKET_DEMO_HPP_

#include <Arduino.h>
#include "packet.hpp"

class PacketDemo {
public:
  bool initialize(PacketQueue& queue, NodeId selfId) {
    for (int i = 0; i < 5; ++i) {
      if (!push_packet(queue, selfId)) return false;
    }
    return true;
  }

  void update(PacketQueue& queue, NodeId selfId, uint32_t nowMs, bool displayReady) {
    if (!started_) {
      if (!displayReady) return;
      lastPopMs_ = lastPushMs_ = nowMs;
      started_ = true;
    }

    // 遅れたイベントも時刻順に処理する。同時刻ならpopしてからpushする。
    for (;;) {
      const uint32_t popElapsed = nowMs - lastPopMs_;
      const uint32_t pushElapsed = nowMs - lastPushMs_;
      if (popElapsed >= 500 &&
          (pushElapsed < 1000 || popElapsed - 500 >= pushElapsed - 1000)) {
        queue.pop();
        lastPopMs_ += 500;
      } else if (pushElapsed >= 1000) {
        if (!push_packet(queue, selfId)) log_e("Demo packet queue full");
        lastPushMs_ += 1000;
      } else {
        break;
      }
    }
  }

private:
  uint16_t nextId_ = 1;
  uint32_t lastPopMs_ = 0;
  uint32_t lastPushMs_ = 0;
  bool started_ = false;

  bool push_packet(PacketQueue& queue, NodeId selfId) {
    // A〜Hの7宛先を選び、自ノードの番号を飛ばす。
    uint8_t destination = random(1, static_cast<uint8_t>(NodeId::P8));
    if (destination >= static_cast<uint8_t>(selfId)) ++destination;
    if (!queue.push({nextId_, static_cast<NodeId>(destination)})) return false;
    ++nextId_;
    return true;
  }
};

#endif // PACKET_DEMO_HPP_
