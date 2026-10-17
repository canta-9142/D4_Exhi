#ifndef PACKET_HPP_
#define PACKET_HPP_

#include <cstddef>
#include <cstdint>
#include <array>
#include "node_id.hpp"

struct Packet {
  uint16_t id;
  NodeId destination;
};

class PacketQueue {
private:
  static constexpr size_t capacity_ = 1024;
  std::array<Packet, capacity_> buffer_{};
  size_t head_ = 0;
  size_t count_ = 0;
  uint64_t pushCount_ = 0;

public:
  bool push(const Packet& packet) {
    if (count_ == capacity_) return false;

    buffer_[(head_ + count_) % capacity_] = packet;
    ++count_;
    ++pushCount_;
    return true;
  }

  bool pop() {
    if (count_ == 0) return false;
    
    head_ = (head_ + 1) % capacity_;
    --count_;
    return true;
  }

  const Packet* at(size_t index) const {
    if (index >= count_) return nullptr;
    return &buffer_[(head_ + index) % capacity_];
  }

  size_t size() const {
    return count_;
  }

  uint64_t pushCount() const {
    return pushCount_;
  }
};

#endif // PACKET_HPP_
