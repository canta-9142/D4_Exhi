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
#include "top_percent.hpp"

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
  bool preview = false;
  FinalSummary summary{};
  uint16_t finishedProgress = 1000;
  bool previewLit = true;
  bool countdownGo = false;
  ResultPage resultPage = ResultPage::Score;
  uint16_t topProgress = 1000;
};

static_assert(std::is_trivially_copyable<DisplaySnapshot>::value, "FreeRTOS queues copy snapshots as bytes");
using PacketDirtyRows = std::bitset<ANIMATION_HEIGHT>;

// STATUSの取得前・同期中・確認表示中は、通常のPlaying画面で接続状態を示す。
inline bool countdown_enabled(const DisplaySnapshot& s) {
  return s.phase == GamePhase::Playing && !s.preview && s.synced && s.selfId != NodeId::None &&
         !s.awaitingStatus && s.status.known;
}

enum class DisplayScreen : uint8_t { Playing, Preparing, Finished, Preview, Countdown };
inline DisplayScreen display_screen(const DisplaySnapshot& s) {
  if (s.preview && s.synced && s.selfId != NodeId::None && s.phase != GamePhase::Unknown) return DisplayScreen::Preview;
  if (countdown_enabled(s) && (s.status.countdown || s.countdownGo)) return DisplayScreen::Countdown;
  if (s.phase == GamePhase::Preparing) return DisplayScreen::Preparing;
  if (s.phase == GamePhase::Finished) return DisplayScreen::Finished;
  return DisplayScreen::Playing;
}
inline bool preparing_screen(const DisplaySnapshot& s) { return display_screen(s) == DisplayScreen::Preparing; }

enum class FinishedState : uint8_t { Syncing, Status, Result, Ready };
inline FinishedState finished_state(const DisplaySnapshot& s) {
  if (!s.synced || s.selfId == NodeId::None) return FinishedState::Syncing;
  if (s.awaitingStatus || !s.status.known) return FinishedState::Status;
  return s.summary.known ? FinishedState::Ready : FinishedState::Result;
}
inline bool same_bests(const FinalSummary& a, const FinalSummary& b) {
  return a.gameBest == b.gameBest && a.gameBestNodes == b.gameBestNodes && a.hourBest == b.hourBest &&
         a.hourBestStart == b.hourBestStart && a.hourBestMine == b.hourBestMine &&
         a.allTimeBest == b.allTimeBest && a.allTimeBestMine == b.allTimeBestMine;
}
inline int32_t displayed_final_score(const DisplaySnapshot& s) {
  const uint64_t left = 1000 - std::min<unsigned>(s.finishedProgress, 1000);
  return static_cast<int32_t>(uint64_t(s.summary.score) * (1000000000 - left * left * left) / 1000000000);
}

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
  bool resultHeader = false, resultBody = false, resultScore = false, resultRank = false, resultAccent = false;
  bool previewBody = false, countdownBody = false;
  bool topBody = false, topCurve = false; // topCurveはベルカーブとマスの両方を描き直す。
  uint8_t topCell = 0; // 点滅で描き直すマスの番号（1〜50）。0は描き直さない。
  bool any() const {
    return resultHeader || resultBody || resultScore || resultRank || resultAccent || topBody || topCurve || topCell || previewBody || countdownBody || full || preparingHeader || preparingBody || preparingBar || preparingArrows || cards.any() || buttons || self || label || score || remaining || ready;
  }
};

inline DisplayChanges display_changes(const DisplaySnapshot& previous, const DisplaySnapshot& next, bool first) {
  DisplayChanges changes;
  first = first || display_screen(previous) != display_screen(next);
  changes.full = first;
  changes.self = first || previous.selfId != next.selfId;
  changes.label = changes.self || previous.phase != next.phase || previous.mode != next.mode ||
                  previous.synced != next.synced || previous.awaitingStatus != next.awaitingStatus;
  if (display_screen(next) == DisplayScreen::Preview) {
    changes.label = false;
    changes.previewBody = first || changes.self || previous.previewLit != next.previewLit;
    return changes;
  }
  if (display_screen(next) == DisplayScreen::Countdown) {
    changes.label = false;
    changes.countdownBody = first || changes.self || previous.status.countdown != next.status.countdown ||
                            previous.countdownGo != next.countdownGo;
    return changes;
  }
  if (display_screen(next) == DisplayScreen::Finished) {
    changes.resultHeader = changes.label || previous.status.known != next.status.known;
    changes.label = false;
    // ページ位置の点の数は上位%の有無で変わる。③は自ノードの色と値が変わったときだけ描き直す。
    changes.resultBody = first || finished_state(previous) != finished_state(next) ||
        previous.resultPage != next.resultPage || previous.summary.topPercent != next.summary.topPercent ||
        previous.summary.bottomHalf != next.summary.bottomHalf ||
        (next.resultPage == ResultPage::Bests && (changes.self || !same_bests(previous.summary, next.summary)));
    if (finished_state(next) == FinishedState::Ready && next.resultPage == ResultPage::TopPercent) {
      changes.topBody = changes.resultBody || changes.self;
      // 下位50%の②には演出も点滅もなく、本文を描くときだけ描く。
      changes.topCurve = changes.topBody || (next.summary.topPercent && previous.topProgress != next.topProgress);
      // 自分のマスは確認表示と同じ500msの周期で点滅させる。
      if (changes.topCurve || previous.previewLit != next.previewLit) changes.topCell = next.summary.topPercent;
    } else if (finished_state(next) == FinishedState::Ready && next.resultPage == ResultPage::Score) {
      changes.resultScore = changes.resultBody || displayed_final_score(previous) != displayed_final_score(next);
      changes.resultRank = changes.resultBody || previous.summary.rank != next.summary.rank ||
          previous.summary.eligible != next.summary.eligible ||
          (previous.finishedProgress != next.finishedProgress &&
           (previous.finishedProgress >= 500 || next.finishedProgress >= 500));
      changes.resultAccent = changes.resultBody || previous.finishedProgress != next.finishedProgress || changes.self;
    }
    return changes;
  }
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
  if (changes.resultBody || changes.previewBody || changes.countdownBody) damage.markRect(0, 81, 480, 239);
  if (changes.resultHeader) damage.markRect(24, 48, 320, 24);
  if (changes.resultScore) damage.markRect(24, 128, 432, 56);
  if (changes.resultRank) damage.markRect(24, 216, 336, 104);
  if (changes.resultAccent) damage.markRect(40, 192, 400, 4);
  if (changes.topBody) damage.markRect(0, 81, 480, 239);
  if (changes.topCurve) {
    damage.markRect(BELL_AREA_X, BELL_AREA_Y, BELL_AREA_W, BELL_AREA_H);
    damage.markRect(GRID_X, GRID_Y, GRID_SIZE, GRID_SIZE);
  }
  if (changes.topCell) damage.markRect(grid_cell_x(changes.topCell - 1), grid_cell_y(changes.topCell - 1), GRID_CELL, GRID_CELL);
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
