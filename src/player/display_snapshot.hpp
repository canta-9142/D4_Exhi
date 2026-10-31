#ifndef DISPLAY_SNAPSHOT_HPP_
#define DISPLAY_SNAPSHOT_HPP_

#include <algorithm>
#include <bitset>
#include <type_traits>
#include "display_layout.hpp"
#include "packet_animation.hpp"
#include "gain_animation.hpp"
#include "playing_effects.hpp"
#include "game_phase.hpp"
#include "player_status.hpp"
#include "frame_damage.hpp"
#include "ready_hold.hpp"
#include "top_percent.hpp"
#include "top_percent_art.hpp"

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
  RoundKind chooseRound = RoundKind::None; // chooseを送り、phase(Playing)を待っている選択
  ResultPage resultPage = ResultPage::Score;
  uint16_t topProgress = 1000;
  TopArtFrame topArt{}; // ②のヘッダーのドット絵
  GainAnimation::Snapshot gains{};
  bool rankFlash = false; // 順位が上がった後、情報欄の数字を緑で描く間
  EffectsFrame effects{};
};

static_assert(std::is_trivially_copyable<DisplaySnapshot>::value, "FreeRTOS queues copy snapshots as bytes");
using PacketDirtyRows = std::bitset<ANIMATION_HEIGHT>;

// STATUSの取得前・同期中・確認表示中は、通常のPlaying画面で接続状態を示す。
inline bool countdown_enabled(const DisplaySnapshot& s) {
  return s.phase == GamePhase::Playing && !s.preview && s.synced && s.selfId != NodeId::None &&
         !s.awaitingStatus && s.status.known;
}

// Closedは停止中（phase=Preparing）の休止中の表示。Idleはボットとしてパケットが流れる待機。
// Standbyは一斉ゲーム方式で、モード選択を終えた準備完了の待機。
enum class DisplayScreen : uint8_t { Playing, Closed, Finished, Preview, Countdown, Idle, Standby, Select };

// 待機・停止中・モード選択で、操作を受け付けられる状態か。取得前は接続・同期の表示を優先する。
enum class LobbyState : uint8_t { Waiting, Auto, Manual, Confirming, Ready };

inline LobbyState lobby_state(const DisplaySnapshot& s) {
  if (s.selfId == NodeId::None || !s.synced || s.awaitingStatus || !s.status.known) return LobbyState::Waiting;
  if (s.mode == ControlMode::Auto) return LobbyState::Auto;
  if (s.phase == GamePhase::Idle && s.status.ready) return LobbyState::Ready;
  // 確認待ちは、モード選択ではchoose、それ以外ではreadyの送信後。
  if (s.phase == GamePhase::Select ? s.chooseRound != RoundKind::None : s.readyPending) return LobbyState::Confirming;
  return LobbyState::Manual;
}

inline DisplayScreen display_screen(const DisplaySnapshot& s) {
  if (s.preview && s.synced && s.selfId != NodeId::None && s.phase != GamePhase::Unknown) return DisplayScreen::Preview;
  if (countdown_enabled(s) && (s.status.countdown || s.countdownGo)) return DisplayScreen::Countdown;
  switch (s.phase) {
    case GamePhase::Preparing: return DisplayScreen::Closed;
    case GamePhase::Idle: return lobby_state(s) == LobbyState::Ready ? DisplayScreen::Standby : DisplayScreen::Idle;
    case GamePhase::Select: return DisplayScreen::Select;
    case GamePhase::Finished: return DisplayScreen::Finished;
    default: return DisplayScreen::Playing;
  }
}
// 本文の全体を描き直す画面（案内の文字が中心で、パケットは流れない）。
inline bool lobby_screen(const DisplaySnapshot& s) {
  const DisplayScreen screen = display_screen(s);
  return screen == DisplayScreen::Closed || screen == DisplayScreen::Standby || screen == DisplayScreen::Select;
}
// 待機で、両端の長押しの案内（右の情報欄の文と、両端の枠の矢印）を出すか。
inline bool idle_guide(const DisplaySnapshot& s) {
  const LobbyState state = lobby_state(s);
  return display_screen(s) == DisplayScreen::Idle && (state == LobbyState::Manual || state == LobbyState::Confirming);
}

// 獲得点を流し、順位を光らせるのは、表示値が揃った通常のPlaying画面だけ。カウントダウン・GO!・確認表示中は流さない。
inline bool gains_enabled(const DisplaySnapshot& s) {
  return countdown_enabled(s) && display_screen(s) == DisplayScreen::Playing;
}
inline const GainAnimation::Snapshot& displayed_gains(const DisplaySnapshot& s) {
  static const GainAnimation::Snapshot empty{};
  return gains_enabled(s) ? s.gains : empty;
}
inline bool displayed_rank_flash(const DisplaySnapshot& s) { return s.rankFlash && s.status.rank && gains_enabled(s); }
// 炎・マスコット・コンボの演出も獲得点と同じく、通常のPlaying画面でだけ出す。
inline const EffectsFrame& displayed_effects(const DisplaySnapshot& s) {
  static const EffectsFrame empty{};
  return gains_enabled(s) ? s.effects : empty;
}
// 左の列の下端に出すコンボ数。出さないときは0。
inline uint16_t displayed_combo(const DisplaySnapshot& s) {
  return gains_enabled(s) && s.status.combo >= COMBO_SHOWN_MIN ? s.status.combo : 0;
}
// 飛んでいる・落ちているマスコットに載せる冠。マスコットがいなければ冠もない。
inline Crown displayed_crown(const DisplaySnapshot& s) {
  const EffectsFrame& f = displayed_effects(s);
  const bool mascots = f.form != MascotForm::None || (f.breakMs != EFFECT_OFF && f.breakMs < BREAK_FALL_MS);
  return mascots ? crown_for_rank(s.status.rank) : Crown::None;
}
inline bool same_left_column(const DisplaySnapshot& a, const DisplaySnapshot& b) {
  return displayed_effects(a) == displayed_effects(b) && displayed_combo(a) == displayed_combo(b) &&
         displayed_crown(a) == displayed_crown(b);
}
inline bool same_right_margin(const DisplaySnapshot& a, const DisplaySnapshot& b) {
  return same_flame(displayed_effects(a), displayed_effects(b));
}
inline bool same_gain_pixels(const GainFloat& previous, const GainFloat& next) {
  if (previous.active != next.active) return false;
  return !next.active || (previous.value == next.value && gain_offset(previous) == gain_offset(next) &&
                          gain_level(previous) == gain_level(next));
}

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
// ②のヘッダーにドット絵を出すか。結果を表示できる②（上位%か下位50%）の間だけ。ゲーム側の進行と描画の両方が使う。
inline bool top_art_page(const DisplaySnapshot& s) {
  return display_screen(s) == DisplayScreen::Finished && finished_state(s) == FinishedState::Ready &&
         s.resultPage == ResultPage::TopPercent && top_art_tier(s.summary);
}
inline TopArtFrame displayed_top_art(const DisplaySnapshot& s) { return top_art_page(s) ? s.topArt : TopArtFrame{}; }
// Finishedの見出しのモード表示（MANUAL・AUTO）を描き直す範囲。②のヘッダーの絵の範囲とは重ねない。
constexpr int RESULT_MODE_X = 24, RESULT_MODE_Y = 48, RESULT_MODE_WIDTH = TOP_ART_X - RESULT_MODE_X, RESULT_MODE_HEIGHT = 24;
inline bool same_top_art(const DisplaySnapshot& a, const DisplaySnapshot& b) {
  return same_top_art(top_art_tier(a.summary), displayed_top_art(a), top_art_tier(b.summary), displayed_top_art(b));
}
inline int32_t displayed_final_score(const DisplaySnapshot& s) {
  const uint64_t left = 1000 - std::min<unsigned>(s.finishedProgress, 1000);
  return static_cast<int32_t>(uint64_t(s.summary.score) * (1000000000 - left * left * left) / 1000000000);
}

// 見出しの隅に出す段階の残り秒数。モード選択で、操作を受け付けられる状態のとき。出さないときは-1。
inline int header_window(const DisplaySnapshot& s) {
  const bool shown = display_screen(s) == DisplayScreen::Select && lobby_state(s) != LobbyState::Waiting;
  return shown && s.status.window >= 1 ? s.status.window : -1;
}

inline const PacketAnimation::Snapshot& displayed_cards(const DisplaySnapshot& s) {
  static const PacketAnimation::Snapshot empty{};
  const bool cardPhase = s.phase == GamePhase::Playing || s.phase == GamePhase::Idle;
  return cardPhase && s.synced && s.selfId != NodeId::None ? s.cards : empty;
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
  bool self = false, label = false, score = false, remaining = false, ready = false, rank = false, gains = false;
  bool leftColumn = false, rightMargin = false; // 炎・マスコット・コンボの演出を描く左の列とカード欄の右の余白
  bool full = false, lobbyHeader = false, lobbyBody = false, headerWindow = false;
  bool idleGuide = false, idleBar = false, idleArrows = false; // 待機の右の情報欄の案内・バーと、両端の枠の矢印
  bool resultHeader = false, resultBody = false, resultScore = false, resultRank = false, resultAccent = false;
  bool previewBody = false, countdownBody = false;
  bool topBody = false, topCurve = false; // topCurveはベルカーブとマスの両方を描き直す。
  uint8_t topCell = 0; // 点滅で描き直すマスの番号（1〜50）。0は描き直さない。
  bool topArt = false; // ②のヘッダーのドット絵の範囲
  bool any() const {
    return resultHeader || resultBody || resultScore || resultRank || resultAccent || topBody || topCurve || topCell || topArt || previewBody || countdownBody || full || lobbyHeader || lobbyBody || headerWindow || idleGuide || idleBar || idleArrows || cards.any() || buttons || self || label || score || remaining || ready || rank || gains || leftColumn || rightMargin;
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
    // ②のヘッダーの絵は、絵が変わったときと、②を離れて消すときに描き直す。
    changes.topArt = !same_top_art(previous, next) || (first && displayed_top_art(next).shown);
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
  if (lobby_screen(next)) {
    const auto state = lobby_state(next);
    changes.lobbyHeader = changes.label || previous.status.known != next.status.known;
    changes.label = false;
    // 準備完了の待機は、選んだ種別（status.round）も本文に出す。
    // 本文が変わるのは、取得中かどうか。操作モードや長押しの状態は、停止中と準備完了の本文に出さない。
    // モード選択は、操作を受け付ける状態（選択・確認待ち・自動）と、選んだ種別が本文に出る。
    const bool select = display_screen(next) == DisplayScreen::Select;
    changes.lobbyBody = first || (select ? lobby_state(previous) != state : (lobby_state(previous) == LobbyState::Waiting) != (state == LobbyState::Waiting)) ||
                        (state == LobbyState::Waiting && changes.lobbyHeader) ||
                        (display_screen(next) == DisplayScreen::Standby && previous.status.round != next.status.round) ||
                        (select && previous.chooseRound != next.chooseRound);
    changes.headerWindow = first || header_window(previous) != header_window(next);
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
  if (display_screen(next) == DisplayScreen::Idle) {
    // 待機は得点・時間・順位・獲得点を持たない。長押しの案内の間は、両端の枠にボタンの代わりに矢印を描く。
    constexpr uint8_t ENDS = ReadyHold::END_BUTTONS;
    const bool arrows = idle_guide(next), wasArrows = idle_guide(previous);
    changes.idleGuide = first || lobby_state(previous) != lobby_state(next);
    changes.idleBar = changes.idleGuide || (arrows && previous.readyHoldMs != next.readyHoldMs);
    changes.idleArrows = arrows && (changes.self || !wasArrows || ((previous.pressedMask ^ next.pressedMask) & ENDS));
    if (arrows) changes.buttons &= static_cast<uint8_t>(~ENDS);
    else if (wasArrows) changes.buttons |= ENDS;
    return changes;
  }
  const bool validity = first || previous.status.known != next.status.known;
  changes.score = validity || previous.status.score != next.status.score;
  changes.remaining = validity || previous.status.remaining != next.status.remaining;
  changes.ready = validity || previous.status.ready != next.status.ready;
  changes.rank = validity || previous.status.rank != next.status.rank ||
                 displayed_rank_flash(previous) != displayed_rank_flash(next);
  // 流れる点は自ノードの色で描くので、自ノードが変わったときも描き直す。
  const auto& oldGains = displayed_gains(previous);
  const auto& newGains = displayed_gains(next);
  changes.gains = first || changes.self;
  for (size_t i = 0; i < newGains.size(); ++i) changes.gains |= !same_gain_pixels(oldGains[i], newGains[i]);
  // 炎とマスコットは約15fpsの1段ごとに進む。演出の状態・コンボ数・冠が変わったときだけ描き直す。
  changes.leftColumn = first || !same_left_column(previous, next);
  changes.rightMargin = first || !same_right_margin(previous, next);
  return changes;
}

inline FrameDamage display_damage(const DisplayChanges& changes, bool first) {
  FrameDamage damage;
  if (first || changes.full) { damage.full(); return damage; }
  if (changes.resultBody || changes.previewBody || changes.countdownBody) damage.markRect(0, 81, 480, 239);
  if (changes.resultHeader) damage.markRect(RESULT_MODE_X, RESULT_MODE_Y, RESULT_MODE_WIDTH, RESULT_MODE_HEIGHT);
  if (changes.resultScore) damage.markRect(24, 128, 432, 56);
  if (changes.resultRank) damage.markRect(24, 216, 336, 104);
  if (changes.resultAccent) damage.markRect(40, 192, 400, 4);
  if (changes.topBody) damage.markRect(0, 81, 480, 239);
  if (changes.topCurve) {
    damage.markRect(BELL_AREA_X, BELL_AREA_Y, BELL_AREA_W, BELL_AREA_H);
    damage.markRect(GRID_X, GRID_Y, GRID_SIZE, GRID_SIZE);
  }
  if (changes.topCell) damage.markRect(grid_cell_x(changes.topCell - 1), grid_cell_y(changes.topCell - 1), GRID_CELL, GRID_CELL);
  if (changes.topArt) damage.markRect(TOP_ART_X, TOP_ART_Y, TOP_ART_WIDTH, TOP_ART_HEIGHT);
  if (changes.lobbyBody) damage.markRect(0, 81, 480, 239);
  if (changes.lobbyHeader) damage.markRect(24, 48, 320, 24);
  if (changes.headerWindow) damage.markRect(HEADER_WINDOW_X, HEADER_WINDOW_Y, HEADER_WINDOW_WIDTH, HEADER_WINDOW_HEIGHT);
  if (changes.idleGuide) damage.markRect(INFO_X, IDLE_GUIDE_Y, INFO_WIDTH, IDLE_GUIDE_HEIGHT);
  else if (changes.idleBar) damage.markRect(IDLE_BAR_X, IDLE_BAR_Y, IDLE_BAR_WIDTH, IDLE_BAR_HEIGHT);
  if (changes.idleArrows) {
    damage.markRect(button_frame_x(0), BUTTON_FRAME_Y, BUTTON_FRAME_WIDTH, BUTTON_FRAME_HEIGHT);
    damage.markRect(button_frame_x(3), BUTTON_FRAME_Y, BUTTON_FRAME_WIDTH, BUTTON_FRAME_HEIGHT);
  }
  for (int row = 0; row < ANIMATION_HEIGHT;) {
    if (!changes.cards[row]) { ++row; continue; }
    const int top = row;
    while (row < ANIMATION_HEIGHT && changes.cards[row]) ++row;
    damage.markRect(ANIMATION_X, ANIMATION_Y + top, ANIMATION_WIDTH, row - top);
  }
  if (changes.leftColumn) damage.markRect(LEFT_COLUMN_X, LEFT_COLUMN_Y, LEFT_COLUMN_WIDTH, LEFT_COLUMN_HEIGHT);
  if (changes.rightMargin) damage.markRect(RIGHT_MARGIN_X, RIGHT_MARGIN_Y, RIGHT_MARGIN_WIDTH, RIGHT_MARGIN_HEIGHT);
  if (changes.self) damage.markRect(361, 1, 119, 79);
  for (unsigned i = 0; i < 4; ++i)
    if (changes.buttons & (1u << i)) damage.markRect(i * 120 + 1, 241, 119, 79);
  if (changes.label) damage.markRect(INFO_X, 81, INFO_WIDTH, INFO_HEADER_HEIGHT);
  if (changes.gains) damage.markRect(INFO_X, GAIN_LANE_Y, INFO_WIDTH, GAIN_LANE_HEIGHT);
  if (changes.score) damage.markRect(366, INFO_SCORE_Y, 72, 24);
  if (changes.remaining) damage.markRect(366, INFO_REMAINING_Y, 72, 24);
  if (changes.ready) damage.markRect(366, INFO_READY_Y, 108, 8);
  if (changes.rank) damage.markRect(INFO_RANK_X, INFO_RANK_Y, INFO_RANK_WIDTH, INFO_RANK_HEIGHT);
  return damage;
}

#endif // DISPLAY_SNAPSHOT_HPP_
