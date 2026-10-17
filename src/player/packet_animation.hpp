#ifndef PACKET_ANIMATION_HPP_
#define PACKET_ANIMATION_HPP_

#include <cstddef>
#include <cstdint>
#include <array>
#include <algorithm>
#include <cmath>
#include "packet.hpp"

constexpr size_t   PACKET_VISIBLE_COUNT = 3;
constexpr int      PACKET_CARD_HEIGHT   = 60;
constexpr int      PACKET_CARD_SPACING  = 80;
constexpr float    PACKET_CARD_SPEED    = 200.0f; // pixels/s
constexpr uint16_t PACKET_FADE_MS       = 200;

constexpr float packet_ease_out_cubic(float t) {
  const float remaining = 1.0f - t;
  return 1.0f - remaining * remaining * remaining;
}

struct PacketCard {
  Packet packet{};
  float y = 0;
  int targetY = 0;
  bool active = false;
  uint16_t fadeMs = PACKET_FADE_MS;

  uint8_t opacity() const {
    return 255 * packet_ease_out_cubic(static_cast<float>(fadeMs) / PACKET_FADE_MS);
  }
};

class PacketAnimation {
public:
  using Snapshot = std::array<PacketCard, PACKET_VISIBLE_COUNT + 1>;

  Snapshot snapshot() const {
    return cards_;
  }

  void update(const PacketQueue& queue, uint32_t elapsedMs, int height) {
    const size_t visibleCount = std::min(queue.size(), PACKET_VISIBLE_COUNT);

    Snapshot next{};
    next[PACKET_VISIBLE_COUNT] = cards_[PACKET_VISIBLE_COUNT];

    MotionSnapshot nextMotions{};
    nextMotions[PACKET_VISIBLE_COUNT] = motions_[PACKET_VISIBLE_COUNT];

    prepare_visible_cards(queue, visibleCount, height, next, nextMotions);
    select_exiting_card(queue, visibleCount, height, next, nextMotions);
    advance_cards(elapsedMs, height, next, nextMotions);

    cards_ = next;
    motions_ = nextMotions;
    previousPushCount_ = queue.pushCount();
  }

private:
  struct Motion {
    float startY = 0;
    uint32_t durationMs = 0;
    uint32_t elapsedMs = 0;
  };

  using MotionSnapshot = std::array<Motion, PACKET_VISIBLE_COUNT + 1>;

  void prepare_visible_cards(const PacketQueue& queue, size_t visibleCount, int height,
                             Snapshot& next, MotionSnapshot& nextMotions) const {
    const int bottomY = height - PACKET_CARD_HEIGHT - 10;
    const uint64_t firstPush = queue.pushCount() - queue.size();

    for (size_t i = 0; i < visibleCount; ++i) {
      const Packet& packet = *queue.at(i);
      const int targetY = bottomY - static_cast<int>(i) * PACKET_CARD_SPACING;
      const auto previous = std::find_if(cards_.begin(),
                                         cards_.end(),
                                         [&](const PacketCard& card) { return card.active && card.packet.id == packet.id; } );

      if (previous != cards_.end()) {
        next[i] = *previous;
        next[i].packet = packet;
        next[i].targetY = targetY;
        nextMotions[i] = (previous->targetY == targetY) ? motions_[previous - cards_.begin()] : start_motion(next[i]);
      }
      else {
        next[i] = {packet, static_cast<float>(targetY), targetY, true};

        if (firstPush + i >= previousPushCount_) {
          next[i].fadeMs = 0;
        }
        else {
          next[i].y -= bottomY + PACKET_CARD_HEIGHT;
        }
        nextMotions[i] = start_motion(next[i]);
      }

      if (next[PACKET_VISIBLE_COUNT].active && next[PACKET_VISIBLE_COUNT].packet.id == packet.id) {
        next[PACKET_VISIBLE_COUNT].active = false;
      }
    }
  }

  void select_exiting_card(const PacketQueue& queue, size_t visibleCount, int height,
                          Snapshot& next, MotionSnapshot& nextMotions) const {
    for (size_t i = 0; i < PACKET_VISIBLE_COUNT; ++i) {
      const PacketCard& previous = cards_[i];
      if (!previous.active) continue;

      bool queued = false;
      for (size_t j = 0; j < visibleCount; ++j) {
        if (queue.at(j)->id == previous.packet.id) queued = true;
      }
      if (queued) continue;

      next[PACKET_VISIBLE_COUNT] = previous;
      next[PACKET_VISIBLE_COUNT].targetY = height;
      nextMotions[PACKET_VISIBLE_COUNT] = start_motion(next[PACKET_VISIBLE_COUNT]);
    }
  }

  static void advance_cards(uint32_t elapsedMs, int height,
                           Snapshot& next, MotionSnapshot& nextMotions) {
    for (size_t i = 0; i < next.size(); ++i) {
      auto& card = next[i];
      if (!card.active) continue;

      const uint32_t remainingMs = PACKET_FADE_MS - card.fadeMs;
      card.fadeMs += std::min(elapsedMs, remainingMs);

      if (card.y != card.targetY) {
        auto& motion = nextMotions[i];
        const uint32_t remainingMotionMs = motion.durationMs - motion.elapsedMs;
        motion.elapsedMs += std::min(elapsedMs, remainingMotionMs);

        if (motion.elapsedMs == motion.durationMs) {
          card.y = card.targetY;
        }
        else {
          const float t = static_cast<float>(motion.elapsedMs) / motion.durationMs;
          card.y = motion.startY + (card.targetY - motion.startY) * packet_ease_out_cubic(t);
        }
      }
      if (card.targetY == height && card.y == height) {
        card.active = false;
      }
    }
  }

  static Motion start_motion(const PacketCard& card) {
    const float durationMs = std::fabs(card.targetY - card.y) * 1000 / PACKET_CARD_SPEED;
    return {card.y, static_cast<uint32_t>(std::ceil(durationMs)), 0};
  }

  Snapshot cards_{};
  MotionSnapshot motions_{};
  uint64_t previousPushCount_ = 0;
};

#endif // PACKET_ANIMATION_HPP_
