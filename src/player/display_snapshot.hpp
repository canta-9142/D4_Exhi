#ifndef DISPLAY_SNAPSHOT_HPP_
#define DISPLAY_SNAPSHOT_HPP_

#include <algorithm>
#include <bitset>
#include <type_traits>
#include "display_layout.hpp"
#include "packet_animation.hpp"
#include "game_phase.hpp"
#include "player_status.hpp"
#include "frame_damage.hpp"
#include "ready_hold.hpp"

struct DisplaySnapshot {
  PacketAnimation::Snapshot cards;
  PlayerStatus status;
  NodeId selfId;
  uint8_t pressedMask;
  GamePhase phase;
  ControlMode mode;
  bool synced;
  bool awaitingStatus;
  uint16_t readyHoldMs = 0;
  bool readyPending = false;
};

static_assert(std::is_trivially_copyable<DisplaySnapshot>::value, "FreeRTOS queues copy snapshots as bytes");
using PacketDirtyRows = std::bitset<ANIMATION_HEIGHT>;

inline bool preparing_screen(const DisplaySnapshot& s) { return s.phase == GamePhase::Preparing; }

enum class PreparingState : uint8_t { Waiting, Auto, Manual, Confirming, Ready };

inline PreparingState preparing_state(const DisplaySnapshot& s) {
  if (s.selfId == NodeId::None || !s.synced || s.awaitingStatus || !s.status.known) return PreparingState::Waiting;
  if (s.mode == ControlMode::Auto) return PreparingState::Auto;
  if (s.status.ready) return PreparingState::Ready;
  if (s.readyPending) return PreparingState::Confirming;
  return PreparingState::Manual;
}

inline const PacketAnimation::Snapshot& displayed_cards(const DisplaySnapshot& s) {
  static const PacketAnimation::Snapshot empty{};
  return s.phase == GamePhase::Playing && s.synced && s.selfId != NodeId::None ? s.cards : empty;
}

inline bool same_card_pixels(const PacketCard& previous, const PacketCard& next) {
  if (previous.active != next.active) return false;
  if (!next.active) return true;
  return static_cast<int>(previous.y) == static_cast<int>(next.y) &&
         previous.packet.id == next.packet.id && previous.packet.destination == next.packet.destination &&
         previous.opacity() == next.opacity();
}

inline void mark_card_rows(PacketDirtyRows& rows, const PacketCard& card) {
  if (!card.active) return;
  const int y = static_cast<int>(card.y);
  const int top = std::max(0, y);
  const int bottom = std::min(ANIMATION_HEIGHT, y + PACKET_CARD_HEIGHT);
  for (int row = top; row < bottom; ++row) rows.set(row);
}

struct DisplayChanges {
  PacketDirtyRows cards;
  uint8_t buttons = 0;
  bool self = false, label = false, score = false, remaining = false, ready = false;
  bool full = false, preparingHeader = false, preparingBody = false, preparingBar = false, preparingArrows = false;
  bool any() const {
    return full || preparingHeader || preparingBody || preparingBar || preparingArrows || cards.any() || buttons || self || label || score || remaining || ready;
  }
};

inline DisplayChanges display_changes(const DisplaySnapshot& previous, const DisplaySnapshot& next, bool first) {
  DisplayChanges changes;
  first = first || preparing_screen(previous) != preparing_screen(next);
  changes.full = first;
  changes.self = first || previous.selfId != next.selfId;
  changes.label = changes.self || previous.phase != next.phase || previous.mode != next.mode ||
                  previous.synced != next.synced || previous.awaitingStatus != next.awaitingStatus;
  if (preparing_screen(next)) {
    const auto state = preparing_state(next);
    changes.preparingHeader = changes.label || previous.status.known != next.status.known;
    changes.label = false;
    changes.preparingBody = first || preparing_state(previous) != state ||
                            (state == PreparingState::Waiting && changes.preparingHeader);
    changes.preparingBar = changes.preparingBody ||
        (state == PreparingState::Manual && previous.readyHoldMs != next.readyHoldMs);
    changes.preparingArrows = changes.preparingBody ||
        (state == PreparingState::Manual && ((previous.pressedMask ^ next.pressedMask) & ReadyHold::END_BUTTONS));
    return changes;
  }
  const auto& oldCards = displayed_cards(previous);
  const auto& newCards = displayed_cards(next);
  if (first) changes.cards.set();
  else for (size_t i = 0; i < newCards.size(); ++i) {
    if (same_card_pixels(oldCards[i], newCards[i])) continue;
    mark_card_rows(changes.cards, oldCards[i]);
    mark_card_rows(changes.cards, newCards[i]);
  }
  changes.buttons = changes.self ? 0x0f : (previous.pressedMask ^ next.pressedMask) & 0x0f;
  const bool validity = first || previous.status.known != next.status.known;
  changes.score = validity || previous.status.score != next.status.score;
  changes.remaining = validity || previous.status.remaining != next.status.remaining;
  changes.ready = validity || previous.status.ready != next.status.ready;
  return changes;
}

inline FrameDamage display_damage(const DisplayChanges& changes, bool first) {
  FrameDamage damage;
  if (first || changes.full) { damage.full(); return damage; }
  if (changes.preparingBody) damage.markRect(0, 81, 480, 239);
  if (changes.preparingHeader) damage.markRect(24, 48, 320, 24);
  if (changes.preparingBar) damage.markRect(40, 152, 400, 32);
  if (changes.preparingArrows) {
    damage.markRect(44, 264, 32, 52);
    damage.markRect(404, 264, 32, 52);
  }
  for (int row = 0; row < ANIMATION_HEIGHT;) {
    if (!changes.cards[row]) { ++row; continue; }
    const int top = row;
    while (row < ANIMATION_HEIGHT && changes.cards[row]) ++row;
    damage.markRect(ANIMATION_X, ANIMATION_Y + top, ANIMATION_WIDTH, row - top);
  }
  if (changes.self) damage.markRect(361, 1, 119, 79);
  for (unsigned i = 0; i < 4; ++i)
    if (changes.buttons & (1u << i)) damage.markRect(i * 120 + 1, 241, 119, 79);
  if (changes.label) damage.markRect(361, 81, 118, 36);
  if (changes.score) damage.markRect(366, 121, 72, 24);
  if (changes.remaining) damage.markRect(366, 174, 72, 24);
  if (changes.ready) damage.markRect(366, 151, 108, 12);
  return damage;
}

#endif // DISPLAY_SNAPSHOT_HPP_
