#ifndef PACKET_HPP_
#define PACKET_HPP_

#include "node_config.hpp"

struct Packet {
  uint16_t id;
  NodeId destination;
};

template <size_t Capacity>
class PacketQueue {
private:
  Packet buffer_[Capacity]{};
  size_t head_ = 0;
  size_t count_ = 0;

public:
  bool push(const Packet& packet) {
    if (count_ == Capacity) return false;

    buffer_[(head_ + count_) % Capacity] = packet;
    ++count_;
    return true;
  }

  bool pop() {
    if (count_ == 0) return false;

    head_ = (head_ + 1) % Capacity;
    --count_;
    return true;
  }

  const Packet* at(size_t index) const {
    if (index >= count_) return nullptr;
    return &buffer_[(head_ + index) % Capacity];
  }

  size_t size() const {
    return count_;
  }
};

#endif // PACKET_HPP_
