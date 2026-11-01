#include <cassert>
#include <cstdio>
#include <cmath>
#include <initializer_list>
#include "../src/player/display_snapshot.hpp"
#include "../src/player/scan_timing.hpp"

int main() {
  DisplaySnapshot previous{};
  previous.selfId = NodeId::P1;
  previous.phase = GamePhase::Playing;
  previous.mode = ControlMode::Manual;
  previous.synced = true;
  previous.status = {123, 120, true, true};
  auto next = previous;
  assert(!display_changes(previous, next, false).any());
  const auto first = display_changes(previous, next, true);
  assert(first.cards.all() && first.buttons == 15 && first.self && first.score && first.remaining && first.ready && first.label);
  next.pressedMask = 5;
  auto changes = display_changes(previous, next, false);
  assert(changes.buttons == 5 && !changes.self && changes.cards.none() && !changes.score);
  next = previous;
  next.status.score = 2147483647;
  changes = display_changes(previous, next, false);
  assert(changes.score && !changes.remaining && !changes.ready && changes.cards.none());
  next = previous;
  next.status.known = false;
  changes = display_changes(previous, next, false);
  assert(changes.score && changes.remaining && changes.ready);
  next = previous;
  next.selfId = NodeId::None;
  changes = display_changes(previous, next, false);
  assert(changes.self && changes.buttons == 15 && changes.label);
  previous.cards[0] = {{1, NodeId::P2}, 10, 10, true, PACKET_FADE_MS};
  next = previous;
  next.cards[0].y = 20;
  changes = display_changes(previous, next, false);
  for (int y = 0; y < ANIMATION_HEIGHT; ++y) assert(changes.cards[y] == (y >= 10 && y < 80));
  next = previous;
  next.cards[0].y = 10.9f;
  assert(!display_changes(previous, next, false).any());
  next = previous;
  next.cards[0].fadeMs = 0;
  assert(display_changes(previous, next, false).cards.count() == PACKET_CARD_HEIGHT);
  next = previous;
  next.cards[0].active = false;
  assert(display_changes(previous, next, false).cards.count() == PACKET_CARD_HEIGHT);
  previous.cards[0].y = -40;
  next = previous;
  next.cards[0].active = false;
  assert(display_changes(previous, next, false).cards.count() == 20);
  previous.cards[0].y = 10;
  auto displayed = previous;
  displayed.cards[0].y = 20;
  next = displayed;
  next.cards[0].y = 30;
  const auto redraw = display_changes(previous, next, false);
  const auto transfer = display_changes(displayed, next, false);
  assert(redraw.cards[10] && !transfer.cards[10] && transfer.cards[20] && transfer.cards[89]);
  const auto damage = display_damage(transfer, false);
  assert(damage.containsPixel(120, 20) && damage.containsPixel(319, 89));
  assert(!damage.containsPixel(400, 200));

  // 停止中は休止中の表示。パケットとボタンは描かず、本文は状態が変わったときだけ描き直す。
  next = displayed;
  next.phase = GamePhase::Preparing;
  next.status.ready = false;
  auto closed = display_changes(displayed, next, false);
  assert(display_screen(next) == DisplayScreen::Closed && lobby_screen(next));
  assert(closed.full && closed.lobbyBody && closed.cards.none() && closed.buttons == 0);
  assert(lobby_state(next) == LobbyState::Manual);
  auto operated = next;
  operated.pressedMask = 9;
  operated.readyHoldMs = 1500;
  operated.readyPending = true;
  assert(!display_changes(next, operated, false).any()); // 停止中は操作を受け付けず、長押しの進捗も出さない。
  operated = next;
  operated.synced = false;
  auto closedWait = display_changes(next, operated, false);
  assert(lobby_state(operated) == LobbyState::Waiting && closedWait.lobbyBody && closedWait.lobbyHeader && !closedWait.full);
  operated.mode = ControlMode::Auto;
  operated.synced = true;
  assert(lobby_state(operated) == LobbyState::Auto && display_screen(operated) == DisplayScreen::Closed);
  assert(display_changes(next, displayed, false).full);

  // 待機は、ボットとしてパケットが流れる画面。受付中は右の情報欄に長押しの案内とバー、両端の枠に矢印を出す。
  auto idle = displayed;
  idle.phase = GamePhase::Idle;
  idle.status = {0, 0, false, true};
  idle.pressedMask = 0;
  assert(display_screen(idle) == DisplayScreen::Idle && !lobby_screen(idle));
  assert(lobby_state(idle) == LobbyState::Manual && idle_guide(idle));
  assert(displayed_cards(idle)[0].active); // 待機でもパケットを流す。
  auto enter = display_changes(displayed, idle, false);
  assert(enter.full && enter.cards.all() && enter.self && enter.idleGuide && enter.idleBar && enter.idleArrows);
  assert(enter.buttons == 0x06); // 両端の枠はボタンの代わりに矢印を描く。
  assert(!enter.score && !enter.remaining && !enter.ready && !enter.rank && !enter.gains);
  assert(!enter.leftColumn && !enter.rightMargin && !enter.lobbyBody);
  assert(display_damage(enter, false).containsPixel(0, 0) && display_damage(enter, false).containsPixel(479, 319));
  assert(!display_changes(idle, idle, false).any());
  auto idleTick = idle;
  idleTick.status.window = 5;
  idleTick.status.score = 99; // 待機は得点・時間を持たない。
  idleTick.status.remaining = 9;
  assert(!display_changes(idle, idleTick, false).any());
  auto holding = idle;
  holding.pressedMask = 1;
  holding.readyHoldMs = 700;
  auto hold = display_changes(idle, holding, false);
  assert(hold.idleBar && hold.idleArrows && !hold.idleGuide && !hold.full && hold.buttons == 0 && hold.cards.none());
  auto holdArea = display_damage(hold, false);
  assert(holdArea.containsPixel(IDLE_BAR_X, IDLE_BAR_Y) && holdArea.containsPixel(60, 300) && holdArea.containsPixel(420, 300));
  assert(!holdArea.containsPixel(INFO_X + 5, 121) && !holdArea.containsPixel(180, 300)); // 案内の文と内側の枠は触れない。
  auto holdMore = holding;
  holdMore.readyHoldMs = 1400;
  hold = display_changes(holding, holdMore, false);
  assert(hold.idleBar && !hold.idleArrows && !hold.idleGuide && hold.buttons == 0);
  assert(display_damage(hold, false).changedTiles() <= 16);
  holdMore.pressedMask = 9;
  hold = display_changes(holding, holdMore, false);
  assert(hold.idleArrows && hold.buttons == 0);
  auto inner = idle;
  inner.pressedMask = 2;
  hold = display_changes(idle, inner, false);
  assert(hold.buttons == 2 && !hold.idleArrows && !hold.idleBar);
  auto confirming = holding;
  confirming.readyPending = true;
  confirming.readyHoldMs = ReadyHold::DURATION_MS;
  assert(lobby_state(confirming) == LobbyState::Confirming && idle_guide(confirming));
  hold = display_changes(holding, confirming, false);
  assert(hold.idleGuide && hold.idleBar && !hold.idleArrows && !hold.full && hold.buttons == 0);
  auto automatic = idle;
  automatic.mode = ControlMode::Auto;
  assert(lobby_state(automatic) == LobbyState::Auto && !idle_guide(automatic)); // ボット固定では案内も矢印も出さない。
  hold = display_changes(idle, automatic, false);
  assert(hold.label && hold.idleGuide && !hold.idleArrows && hold.buttons == 0x09 && !hold.full); // 端の枠はボタンに戻す。
  automatic.readyHoldMs = 900;
  automatic.pressedMask = 9;
  assert(display_changes(automatic, automatic, false).buttons == 0);
  auto autoPressed = automatic;
  autoPressed.pressedMask = 0;
  assert(display_changes(automatic, autoPressed, false).buttons == 0x09 && !display_changes(automatic, autoPressed, false).idleArrows);
  auto unsynced = idle;
  unsynced.synced = false;
  assert(lobby_state(unsynced) == LobbyState::Waiting && !idle_guide(unsynced) && !displayed_cards(unsynced)[0].active);
  hold = display_changes(idle, unsynced, false);
  assert(hold.idleGuide && hold.label && hold.cards.any() && hold.buttons == 0x09 && !hold.idleArrows);
  auto unknown = idle;
  unknown.status.known = false;
  assert(lobby_state(unknown) == LobbyState::Waiting && !idle_guide(unknown));
  assert(BUTTON_FRAME_Y == 241 && button_frame_x(0) == 1 && button_frame_x(3) == 361);
  assert(IDLE_GUIDE_Y > 112 && IDLE_GUIDE_Y + IDLE_GUIDE_HEIGHT == 240 && INFO_X + INFO_WIDTH == 479);
  assert(IDLE_BAR_Y >= IDLE_GUIDE_Y && IDLE_BAR_Y + IDLE_BAR_HEIGHT < 240 && IDLE_BAR_X + IDLE_BAR_WIDTH <= 480);
  // 一斉ゲーム方式で準備完了の待機は、選んだ種別と開始待ちの案内（パケットは流さない）。
  auto standby = idle;
  standby.status.ready = true;
  standby.status.round = RoundKind::Normal;
  assert(display_screen(standby) == DisplayScreen::Standby && lobby_screen(standby) && lobby_state(standby) == LobbyState::Ready);
  assert(!idle_guide(standby));
  auto standbyEnter = display_changes(idle, standby, false);
  assert(standbyEnter.full && standbyEnter.lobbyBody && standbyEnter.cards.none() && standbyEnter.buttons == 0);
  auto standbyTutorial = standby;
  standbyTutorial.status.round = RoundKind::Tutorial;
  assert(display_changes(standby, standbyTutorial, false).lobbyBody);
  standbyTutorial.pressedMask = 15;
  standbyTutorial.readyHoldMs = 800;
  assert(!display_changes(standbyTutorial, standbyTutorial, false).any());
  auto standbyAuto = standby;
  standbyAuto.mode = ControlMode::Auto; // ボット固定は準備完了の表示にしない。
  assert(display_screen(standbyAuto) == DisplayScreen::Idle && lobby_state(standbyAuto) == LobbyState::Auto);

  // モード選択: 左から2番目の枠にTUTORIAL、3番目の枠にNORMAL。見出しの隅に残り秒数を出す。
  auto select = idle;
  select.phase = GamePhase::Select;
  select.status = {0, 0, false, true};
  select.status.window = 20;
  assert(display_screen(select) == DisplayScreen::Select && lobby_screen(select) && !idle_guide(select));
  assert(lobby_state(select) == LobbyState::Manual && header_window(select) == 20);
  assert(!displayed_cards(select)[0].active); // モード選択ではパケットを流さない。
  auto selectEnter = display_changes(idle, select, false);
  assert(selectEnter.full && selectEnter.lobbyBody && selectEnter.headerWindow && selectEnter.cards.none());
  assert(selectEnter.buttons == 0 && !selectEnter.idleGuide && !selectEnter.idleArrows);
  auto selectArea = display_damage(selectEnter, false);
  assert(selectArea.containsPixel(0, 0) && selectArea.containsPixel(479, 319));
  // 残り秒数は1秒ごとに、見出しの隅だけを描き直す。
  auto selectTick = select;
  selectTick.status.window = 19;
  auto sd = display_changes(select, selectTick, false);
  assert(sd.headerWindow && !sd.lobbyBody && !sd.lobbyHeader && !sd.full && !sd.label);
  auto tickArea = display_damage(sd, false);
  assert(tickArea.containsPixel(HEADER_WINDOW_X, HEADER_WINDOW_Y) && !tickArea.containsPixel(0, 81) && !tickArea.containsPixel(24, 56));
  assert(!tickArea.containsPixel(360, 0) && !tickArea.containsPixel(24, 60)); // 自ノードの札とモード表示の行には触れない。
  assert(HEADER_WINDOW_X + HEADER_WINDOW_WIDTH <= TOP_ART_X && HEADER_WINDOW_Y + HEADER_WINDOW_HEIGHT < 80);
  assert(HEADER_WINDOW_Y + HEADER_WINDOW_HEIGHT <= RESULT_MODE_Y); // モード表示の行とも重ねない。
  selectTick.status.window = WINDOW_WARNING_SECONDS; // 10秒以下は注意色。色も秒数から決まるので、秒数の変化で描き直す。
  assert(display_changes(select, selectTick, false).headerWindow && header_window(selectTick) == 10);
  selectTick.status.window = 0; // 時間制限がない（一斉ゲーム方式）。残り秒数は出さない。
  assert(header_window(selectTick) == -1 && display_changes(select, selectTick, false).headerWindow);
  assert(!display_changes(selectTick, selectTick, false).any());
  auto selectKnown = select;
  selectKnown.status.known = false;
  assert(lobby_state(selectKnown) == LobbyState::Waiting && header_window(selectKnown) == -1);
  sd = display_changes(select, selectKnown, false);
  assert(sd.lobbyBody && sd.headerWindow && sd.lobbyHeader && !sd.full);
  selectKnown = select;
  selectKnown.synced = false;
  assert(lobby_state(selectKnown) == LobbyState::Waiting && header_window(selectKnown) == -1);
  // 押下・長押しの進捗は、モード選択の画面を描き直さない（選択は押下の立ち上がりでchooseを送る）。
  auto selectPressed = select;
  selectPressed.pressedMask = 6;
  selectPressed.readyHoldMs = 1200;
  assert(!display_changes(select, selectPressed, false).any());
  // 選んだ枠を強調し、確認待ちの間は本文を確認中の表示にする。選び直しで強調する枠も描き直す。
  auto chosen = select;
  chosen.chooseRound = RoundKind::Tutorial;
  assert(lobby_state(chosen) == LobbyState::Confirming);
  sd = display_changes(select, chosen, false);
  assert(sd.lobbyBody && !sd.headerWindow && !sd.full);
  auto chosenNormal = chosen;
  chosenNormal.chooseRound = RoundKind::Normal;
  assert(display_changes(chosen, chosenNormal, false).lobbyBody);
  assert(display_changes(chosenNormal, select, false).lobbyBody);
  chosenNormal.readyPending = true; // モード選択の確認待ちはchooseだけで決まる。readyは関係しない。
  assert(!display_changes(chosen, chosen, false).any());
  auto readyOnly = select;
  readyOnly.readyPending = true;
  assert(lobby_state(readyOnly) == LobbyState::Manual && !display_changes(select, readyOnly, false).any());
  auto selectAuto = select;
  selectAuto.mode = ControlMode::Auto;
  assert(lobby_state(selectAuto) == LobbyState::Auto && display_screen(selectAuto) == DisplayScreen::Select);
  sd = display_changes(select, selectAuto, false);
  assert(sd.lobbyBody && sd.lobbyHeader && !sd.full);
  // 選択を終えてPlayingに移ると、パケットの画面へ全描画で戻る（練習・通常とも）。
  auto roundStart = chosen;
  roundStart.phase = GamePhase::Playing;
  roundStart.chooseRound = RoundKind::None;
  assert(display_screen(roundStart) == DisplayScreen::Playing && display_changes(chosen, roundStart, false).full);
  auto timeout = chosen;
  timeout.phase = GamePhase::Idle;
  timeout.chooseRound = RoundKind::None;
  assert(display_screen(timeout) == DisplayScreen::Idle && display_changes(chosen, timeout, false).full);

  // 練習ラウンド: パケットが降る画面に、TUTORIALの札とPRACTICEを加える。得点・順位・演出は出さない。
  auto normalRound = displayed;
  normalRound.pressedMask = 0;
  normalRound.status = {40, 70, false, true};
  normalRound.status.round = RoundKind::Normal;
  assert(display_screen(normalRound) == DisplayScreen::Playing);
  auto tutorial = normalRound;
  tutorial.status = {0, 0, false, true};
  tutorial.status.round = RoundKind::Tutorial;
  tutorial.status.window = 30;
  assert(display_screen(tutorial) == DisplayScreen::Practice && !lobby_screen(tutorial) && practice_round(tutorial));
  assert(displayed_cards(tutorial)[0].active && !gains_enabled(tutorial)); // 練習でもパケットは流れるが、獲得点・演出は流さない。
  // 表示値が揃うまでは、練習の画面にしない（接続・同期の表示を優先する）。
  auto tutorialWait = tutorial;
  tutorialWait.awaitingStatus = true;
  assert(display_screen(tutorialWait) == DisplayScreen::Playing);
  tutorialWait = tutorial;
  tutorialWait.synced = false;
  assert(display_screen(tutorialWait) == DisplayScreen::Playing);
  tutorialWait = tutorial;
  tutorialWait.status.known = false;
  assert(display_screen(tutorialWait) == DisplayScreen::Playing);
  tutorialWait = tutorial;
  tutorialWait.status.round = RoundKind::None;
  assert(display_screen(tutorialWait) == DisplayScreen::Playing);
  tutorialWait = tutorial;
  tutorialWait.preview = true;
  assert(display_screen(tutorialWait) == DisplayScreen::Preview);
  tutorialWait = tutorial;
  tutorialWait.phase = GamePhase::Finished;
  assert(display_screen(tutorialWait) == DisplayScreen::Finished); // 一斉ゲーム方式の練習の終了は結果の画面。
  tutorialWait = tutorial;
  tutorialWait.status.countdown = 3; // 練習もカウントダウンの数字を出す。
  assert(display_screen(tutorialWait) == DisplayScreen::Countdown);
  tutorialWait.status.countdown = 0;
  tutorialWait.countdownGo = true;
  assert(display_screen(tutorialWait) == DisplayScreen::Countdown);
  // ゲーム側は練習の間だけ、練習用キューの先頭の宛先から帯を判定する。自ノードA、宛先D（参考画像の1）。
  tutorial.hint = tutorial_hint(NodeId::P1, NodeId::P4);
  assert(tutorial.hint.bands[0].cells[3] == HintCell::Lit && tutorial.hint.bands[1].cells[3] == HintCell::Unlinked);
  auto toPractice = display_changes(normalRound, tutorial, false);
  assert(toPractice.full && toPractice.ready && toPractice.remaining && toPractice.self && toPractice.label);
  assert(!toPractice.score && !toPractice.rank && !toPractice.gains && !toPractice.leftColumn && !toPractice.rightMargin);
  assert(toPractice.buttons == 0x0f && toPractice.cards.all());
  assert(toPractice.hintBands == 0x06 && toPractice.botBars == 0x06); // 練習に入ると、Botの2つのボタンに帯とバーを描く。
  assert(displayed_hint(tutorial) == tutorial.hint);
  // 練習の外では、帯とバーを出さない（通常のPlaying・カウントダウン・結果）。
  auto hintOutside = normalRound;
  hintOutside.hint = tutorial.hint;
  hintOutside.status.botsKnown = true;
  hintOutside.status.bots[0] = 4;
  assert(displayed_hint(hintOutside) == TutorialHint{} && displayed_bot_load(hintOutside, 0) == -1);
  assert(!display_changes(normalRound, hintOutside, false).any());
  assert(display_changes(tutorial, normalRound, false).hintBands == 0); // 出るときは全描画で通常の画面に戻す。
  hintOutside = tutorial;
  hintOutside.status.countdown = 2;
  assert(displayed_hint(hintOutside) == TutorialHint{} && displayed_bot_load(hintOutside, 1) == -1);
  assert(display_changes(tutorial, normalRound, false).full); // 練習を出ると通常の画面へ全描画で戻る。
  assert(!display_changes(tutorial, tutorial, false).any());
  // 時間欄: 30秒から毎秒表示を更新し、残り10秒以下は注意色にする。
  assert(practice_time(tutorial.status) == 30 && !practice_time_warn(tutorial.status));
  auto timed = tutorial;
  for (unsigned window = 30; window > 11; --window) {
    timed.status.window = static_cast<uint8_t>(window - 1);
    assert(display_changes(tutorial, timed, false).remaining && practice_time(timed.status) == int(window - 1));
    assert(!practice_time_warn(timed.status));
    tutorial.status.window = static_cast<uint8_t>(window - 1);
  }
  tutorial.status.window = 11;
  timed = tutorial;
  timed.status.window = 10;
  auto timeDiff = display_changes(tutorial, timed, false);
  assert(timeDiff.remaining && !timeDiff.ready && !timeDiff.label && !timeDiff.full && !timeDiff.score);
  assert(practice_time(timed.status) == 10 && practice_time_warn(timed.status));
  auto timeArea = display_damage(timeDiff, false);
  assert(timeArea.containsPixel(366, INFO_REMAINING_Y) && timeArea.containsPixel(438, INFO_REMAINING_Y + 23));
  assert(!timeArea.containsPixel(366, INFO_SCORE_Y) && !timeArea.containsPixel(0, 0) && !timeArea.containsPixel(180, 100));
  tutorial.status.window = 10;
  timed.status.window = 9;
  assert(display_changes(tutorial, timed, false).remaining && practice_time(timed.status) == 9);
  timed.status.window = 1;
  assert(practice_time(timed.status) == 1 && practice_time_warn(timed.status));
  timed.status.window = 0; // 練習の終わり（phase(Idle)が届く直前）。連続運転ではremainingが0なので--。
  assert(practice_time(timed.status) == -1 && !practice_time_warn(timed.status));
  timed.status.remaining = 37; // 一斉ゲーム方式ではwindowが0で、ゲームの残り秒数を出す（注意色にしない）。
  assert(practice_time(timed.status) == 37 && !practice_time_warn(timed.status));
  timed.status.remaining = 120;
  assert(practice_time(timed.status) == 120);
  // 得点・獲得点・コンボ・順位が来ても、練習の画面は描き直さない。
  timed = tutorial;
  timed.status.score = 1234;
  timed.status.rank = 3;
  timed.status.combo = 25;
  timed.status.ready = true;
  timed.rankFlash = true;
  timed.gains[0] = {15, 0, true};
  assert(!display_changes(tutorial, timed, false).any());
  // ボタンとカードは、通常のPlayingと同じ規則で描き直す。
  timed = tutorial;
  timed.pressedMask = 6;
  auto buttonDiff = display_changes(tutorial, timed, false);
  assert(buttonDiff.buttons == 6 && buttonDiff.cards.none() && !buttonDiff.remaining);
  // Botのボタンを描き直すと塗りで帯とバーが消えるので、そのボタンの帯とバーも描き直す。両端のボタンには帯がない。
  assert(buttonDiff.hintBands == 6 && buttonDiff.botBars == 6);
  timed.pressedMask = 4;
  buttonDiff = display_changes(tutorial, timed, false);
  assert(buttonDiff.buttons == 4 && buttonDiff.hintBands == 4 && buttonDiff.botBars == 4);
  timed.pressedMask = 9;
  buttonDiff = display_changes(tutorial, timed, false);
  assert(buttonDiff.buttons == 9 && buttonDiff.hintBands == 0 && buttonDiff.botBars == 0);
  timed = tutorial;
  timed.cards[0].y = 30;
  assert(display_changes(tutorial, timed, false).cards.any());
  timed = tutorial;
  timed.selfId = NodeId::P3;
  buttonDiff = display_changes(tutorial, timed, false);
  assert(buttonDiff.self && buttonDiff.buttons == 0x0f && buttonDiff.label && !buttonDiff.full);
  assert(buttonDiff.hintBands == 0x06 && buttonDiff.botBars == 0x06);

  // 練習のヒントの帯: 判定結果（先頭パケットの宛先）が変わったときだけ、帯の範囲を描き直す。
  timed = tutorial;
  timed.cards[0].packet.id = 99; // 宛先が同じパケットに替わっても、帯は同じなので描き直さない。
  assert(display_changes(tutorial, timed, false).hintBands == 0);
  timed = tutorial;
  timed.hint = tutorial_hint(NodeId::P1, NodeId::P7); // D→G。Iの帯もKの帯も変わる。
  auto hintDiff = display_changes(tutorial, timed, false);
  assert(hintDiff.hintBands == 0x06 && !hintDiff.botBars && !hintDiff.buttons && hintDiff.cards.none());
  assert(!hintDiff.remaining && !hintDiff.ready && !hintDiff.label && !hintDiff.full);
  auto hintArea = display_damage(hintDiff, false);
  assert(hintArea.containsPixel(hint_band_x(1), HINT_BAND_Y) &&
         hintArea.containsPixel(hint_band_x(2) + HINT_BAND_WIDTH - 1, HINT_BAND_Y + HINT_CELL_HEIGHT - 1));
  assert(!hintArea.containsPixel(button_letter_x(1) + 15, BUTTON_LETTER_Y + 20)); // ボタンの文字は描き直さない。
  assert(!hintArea.containsPixel(10, HINT_BAND_Y) && !hintArea.containsPixel(470, HINT_BAND_Y) && !hintArea.containsPixel(180, 100));
  timed.hint = tutorial_hint(NodeId::P1, NodeId::P2); // D→B（隣接）。Iの帯のDが消えるだけで、Kの帯は変わらない。
  assert(display_changes(tutorial, timed, false).hintBands == 0x02);
  auto emptyFifo = timed;
  emptyFifo.hint = tutorial_hint(NodeId::P1, NodeId::None); // FIFOが空でも光らないので、隣接宛てと同じ帯。
  assert(!display_changes(timed, emptyFifo, false).any());
  assert(display_changes(emptyFifo, tutorial, false).hintBands == 0x02);

  // Botの混雑: botsが変わったときだけ、バーとFULLの札を描き直す。botsが来るまでは出さない。
  assert(displayed_bot_load(tutorial, 0) == -1 && displayed_bot_load(tutorial, 1) == -1);
  auto loaded = tutorial;
  loaded.status.botsKnown = true;
  loaded.status.bots[0] = 1; // I（B1）
  loaded.status.bots[1] = 3; // J（B2）。自ノードAの下部にはない。
  loaded.status.bots[2] = 0; // K（B3）
  auto barDiff = display_changes(tutorial, loaded, false);
  assert(barDiff.botBars == 0x06 && !barDiff.hintBands && !barDiff.buttons && !barDiff.remaining && barDiff.cards.none());
  assert(displayed_bot_load(loaded, 0) == 1 && displayed_bot_load(loaded, 1) == 0);
  assert(bot_load_color(displayed_bot_load(loaded, 0)) == BOT_LOAD_BLUE && !bot_load_full(displayed_bot_load(loaded, 0)));
  auto busy = loaded;
  busy.status.bots[1] = 4; // 下部にないBotの変化では描き直さない。
  assert(!display_changes(loaded, busy, false).any());
  busy = loaded;
  busy.status.bots[2] = 4; // Kが満杯。左から3番目のボタンだけ、赤のバーとFULL。
  barDiff = display_changes(loaded, busy, false);
  assert(barDiff.botBars == 0x04 && !barDiff.hintBands);
  assert(bot_load_color(displayed_bot_load(busy, 1)) == BOT_LOAD_RED && bot_load_full(displayed_bot_load(busy, 1)));
  auto barArea = display_damage(barDiff, false);
  assert(barArea.containsPixel(button_frame_x(2), BOT_BAR_Y) && barArea.containsPixel(button_frame_x(2) + BUTTON_FRAME_WIDTH - 1, BOT_BAR_Y));
  assert(barArea.containsPixel(bot_full_x(2), BOT_FULL_Y) && barArea.containsPixel(bot_full_x(2) + BOT_FULL_WIDTH - 1, BOT_FULL_Y + BOT_FULL_HEIGHT - 1));
  assert(!barArea.containsPixel(button_letter_x(2) + 15, BUTTON_LETTER_Y + 8) && !barArea.containsPixel(130, 300));
  // 帯と混雑は独立。宛先が変わってもバーは描き直さず、使用量が変わっても帯は描き直さない。
  auto both = busy;
  both.hint = tutorial_hint(NodeId::P1, NodeId::P7);
  barDiff = display_changes(busy, both, false);
  assert(barDiff.hintBands == 0x06 && barDiff.botBars == 0);
  both.status.window = 59; // 毎秒の通知（同じbots）では描き直さない。
  assert(display_changes(busy, both, false).botBars == 0);
  auto lost = busy;
  lost.status.botsKnown = false; // botsのない通知ではバーを消す。
  assert(display_changes(busy, lost, false).botBars == 0x06 && displayed_bot_load(lost, 1) == -1);
  // 自ノードが変わると、下部のBotも変わる（C: J・K）。
  auto otherSelf = busy;
  otherSelf.selfId = NodeId::P3;
  assert(displayed_bot_load(otherSelf, 0) == 3 && displayed_bot_load(otherSelf, 1) == 4);
  // 札は左上、キュー位置の数字3より上。左の列の演出は出さない。
  assert(PRACTICE_TAG_X >= LEFT_COLUMN_X && PRACTICE_TAG_X + PRACTICE_TAG_WIDTH <= ANIMATION_X);
  assert(PRACTICE_TAG_Y >= 0 && PRACTICE_TAG_Y + PRACTICE_TAG_HEIGHT <= 28);
  assert(INFO_READY_Y + 8 <= INFO_REMAINING_Y);

  auto result = displayed;
  result.phase = GamePhase::Finished;
  result.summary = {INT32_MAX, 1, true, true, 1, true};
  assert(display_changes(displayed, result, false).full);
  assert(finished_state(result) == FinishedState::Ready);
  FinishedAnimation animation;
  result.finishedProgress = animation.update(result.summary, true, UINT32_MAX - 500);
  assert(result.finishedProgress == 0 && displayed_final_score(result) == 0);
  result.finishedProgress = animation.update(result.summary, true, 99);
  assert(result.finishedProgress == 500 && displayed_final_score(result) > INT32_MAX / 2);
  result.finishedProgress = animation.update(result.summary, true, 699);
  assert(result.finishedProgress == 1000 && displayed_final_score(result) == INT32_MAX);
  assert(animation.update(result.summary, true, 2000) == 1000); // 重複は再演しない。
  auto moving = result;
  moving.finishedProgress = 750;
  auto diff = display_changes(result, moving, false);
  assert(diff.resultScore && diff.resultRank && diff.resultAccent && !diff.full && !diff.resultBody);
  auto area = display_damage(diff, false);
  assert(area.containsPixel(24, 128) && area.containsPixel(359, 239) && !area.containsPixel(0, 0));
  // 演出中もボタン枠（ページ送りと「もう一度遊ぶ」の案内）は描かない。順位の欄はボタン枠（y=241〜）の上に収める。
  assert(!area.containsPixel(300, 300) && !area.containsPixel(60, 300) && !area.containsPixel(420, 300) && !area.containsPixel(180, 250));
  assert(RESULT_RANK_Y + RESULT_RANK_HEIGHT <= BUTTON_FRAME_Y && RESULT_RANK_X + RESULT_RANK_WIDTH <= 360);
  moving.summary.eligible = false;
  moving.summary.rank = 0;
  assert(display_changes(result, moving, false).resultRank);
  moving.synced = false;
  assert(finished_state(moving) == FinishedState::Syncing);
  assert(display_changes(result, moving, false).resultBody);
  assert(animation.update(result.summary, false, 3000) == 1000);
  assert(animation.update(result.summary, true, 3001) == 1000);
  // 上位%は2ページ目に出し、ページ・塗りの進捗・自分のマスの点滅ごとに必要な範囲だけを描く。
  auto ranked = result;
  ranked.finishedProgress = 1000;
  ranked.summary.topPercent = 12;
  diff = display_changes(result, ranked, false);
  assert(diff.resultBody && diff.resultRank && !diff.topBody && !diff.full); // 1ページ目に送りの案内を足す。
  auto topPage = ranked;
  topPage.resultPage = ResultPage::TopPercent;
  diff = display_changes(ranked, topPage, false);
  assert(diff.resultBody && diff.topBody && !diff.full && !diff.resultScore && !diff.resultRank && !diff.resultAccent);
  area = display_damage(diff, false);
  assert(area.containsPixel(0, 81) && area.containsPixel(479, 319) && !area.containsPixel(0, 0));
  auto blink = topPage;
  blink.previewLit = false;
  diff = display_changes(topPage, blink, false);
  assert(!diff.topBody && !diff.topCurve && diff.topCell == 12);
  area = display_damage(diff, false);
  assert(area.changedTiles() <= 4 && area.containsPixel(grid_cell_x(11), grid_cell_y(11)));
  auto growing = topPage;
  growing.topProgress = 400;
  diff = display_changes(topPage, growing, false);
  assert(!diff.topBody && diff.topCurve && diff.topCell == 12 && !diff.resultBody);
  area = display_damage(diff, false);
  assert(area.containsPixel(BELL_X, BELL_BASE) && area.containsPixel(GRID_X + GRID_SIZE - 1, GRID_Y + GRID_SIZE - 1));
  assert(!area.containsPixel(240, 88) && !area.containsPixel(420, 300)); // 文字とボタン枠は描かない。
  assert(BELL_AREA_Y + BELL_AREA_H <= 240 && GRID_Y + GRID_SIZE <= 240); // ボタン枠より上に収める。
  auto still = topPage;
  still.finishedProgress = 300;
  still.pressedMask = 1;
  assert(!display_changes(topPage, still, false).any()); // 1ページ目の演出は描かない。
  auto syncing = topPage;
  syncing.synced = false;
  assert(display_changes(topPage, syncing, false).resultBody && !display_changes(topPage, syncing, false).topBody);
  assert(display_changes(syncing, topPage, false).topBody); // 復旧で2ページ目を描き直す。
  assert(top_percent_z_milli(1) == 2326 && top_percent_z_milli(5) == 1645 && top_percent_z_milli(10) == 1282);
  assert(top_percent_z_milli(25) == 674 && top_percent_z_milli(50) == 0);
  for (uint8_t p = 2; p <= 50; ++p) assert(top_percent_z_milli(p) < top_percent_z_milli(p - 1));
  assert(bell_x(0) == BELL_X + BELL_WIDTH / 2 && bell_height(BELL_WIDTH / 2) == BELL_HEIGHT);
  assert(bell_height(0) == 0 && bell_height(BELL_WIDTH - 1) == 0);
  assert(top_fill_x(bell_x(0), 0) == BELL_X + BELL_WIDTH && top_fill_x(bell_x(0), 1000) == bell_x(0));
  assert(top_filled_cells(12, 0) == 0 && top_filled_cells(12, 1000) == 12 && top_filled_cells(50, 1000) == 50);

  // 右端のボタンで①→②→③→①と送る。②の演出は通常の終了遷移で初めて開いたときだけ。
  ResultPager pager;
  FinalSummary top = ranked.summary;
  pager.update(top, true, true, false, 0);
  assert(pager.page() == ResultPage::Score && pager.progress() == 1000);
  pager.update(top, true, true, true, 10);
  assert(pager.page() == ResultPage::TopPercent && pager.progress() == 0);
  pager.update(top, true, true, false, 10 + ResultPager::INTRO_MS / 2);
  assert(pager.progress() == 500);
  pager.update(top, true, true, true, 20 + ResultPager::INTRO_MS / 2);
  assert(pager.page() == ResultPage::Bests && pager.progress() == 1000); // 演出中に押すと演出を終えて進む。
  pager.update(top, true, true, true, 30 + ResultPager::INTRO_MS / 2);
  assert(pager.page() == ResultPage::Score && pager.progress() == 1000); // ③の次は①。
  pager.update(top, true, true, true, 40 + ResultPager::INTRO_MS / 2);
  assert(pager.page() == ResultPage::TopPercent && pager.progress() == 1000); // 2回目は静止画。
  pager.update(top, true, false, true, 2000);
  assert(pager.page() == ResultPage::TopPercent); // 再同期中は押下を無視し、ページを保つ。
  pager.update(top, true, true, false, 2001);
  assert(pager.page() == ResultPage::TopPercent);
  auto renewed = top;
  renewed.revision = 2;
  pager.update(renewed, true, true, false, 2002);
  assert(pager.page() == ResultPage::Score); // syncが変わった結果（別の世代）では①へ戻す。
  pager.update(renewed, true, true, true, 2003);
  assert(pager.page() == ResultPage::TopPercent && pager.progress() == 0);
  pager.update(renewed, true, false, false, 2004);
  assert(pager.progress() == 1000); // 演出中の再同期で取り消し、復旧後は静止画。
  pager.update(renewed, true, true, false, 2005);
  assert(pager.page() == ResultPage::TopPercent && pager.progress() == 1000);
  pager.update(renewed, true, true, true, 2006);
  assert(pager.page() == ResultPage::Bests);
  pager.update(renewed, false, false, false, 2007);
  assert(pager.page() == ResultPage::Score); // Finishedから離れると①。
  renewed.revision = 3;
  renewed.animate = false;
  pager.update(renewed, true, true, true, UINT32_MAX);
  assert(pager.page() == ResultPage::TopPercent && pager.progress() == 1000); // 再接続後の結果は演出しない。
  pager.update(renewed, true, true, true, 0);
  assert(pager.page() == ResultPage::Bests);
  renewed.known = false;
  pager.update(renewed, true, false, false, 1);
  assert(pager.page() == ResultPage::Score); // identify・config・Resetで結果を破棄したとき。
  // 下位50%・対象外は②を飛ばして①と③を行き来する。
  auto unranked = renewed;
  unranked.known = true;
  unranked.revision = 4;
  unranked.topPercent = 0;
  assert(result_page_count(unranked) == 2 && result_page_count(top) == 3);
  pager.update(unranked, true, true, true, 10);
  assert(pager.page() == ResultPage::Bests && result_page_index(unranked, pager.page()) == 1);
  pager.update(unranked, true, true, true, 20);
  assert(pager.page() == ResultPage::Score && result_page_index(unranked, pager.page()) == 0);
  assert(result_page_index(top, ResultPage::TopPercent) == 1 && result_page_index(top, ResultPage::Bests) == 2);
  pager.update(unranked, true, true, false, 30);
  assert(pager.page() == ResultPage::Score); // 押さなければ動かない。
  // 下位50%は②に「下位50%」だけを出す。値がないので演出しない。
  auto bottom = unranked;
  bottom.revision = 5;
  bottom.animate = true;
  bottom.bottomHalf = true;
  assert(has_top_page(bottom) && result_page_count(bottom) == 3);
  pager.update(bottom, true, true, true, 40);
  assert(pager.page() == ResultPage::TopPercent && pager.progress() == 1000);
  assert(result_page_index(bottom, pager.page()) == 1);
  pager.update(bottom, true, true, true, 50);
  assert(pager.page() == ResultPage::Bests && result_page_index(bottom, pager.page()) == 2);
  pager.update(bottom, true, true, true, 60);
  assert(pager.page() == ResultPage::Score);
  // 2ページ目の間は1ページ目の終了演出を止め、戻っても再演しない。
  FinishedAnimation scoreAnimation;
  top.revision = 9;
  assert(scoreAnimation.update(top, true, 0) == 0);
  assert(scoreAnimation.update(top, false, 100) == 1000);
  assert(scoreAnimation.update(top, true, 200) == 1000);

  // 下位50%の②は本文を1度だけ描き、点滅や進捗では描き直さない。
  auto bottomPage = ranked;
  bottomPage.summary.topPercent = 0;
  bottomPage.summary.bottomHalf = true;
  bottomPage.resultPage = ResultPage::TopPercent;
  diff = display_changes(ranked, bottomPage, false);
  assert(diff.resultBody && diff.topBody && !diff.topCell && !diff.full);
  auto bottomStill = bottomPage;
  bottomStill.previewLit = !bottomPage.previewLit;
  bottomStill.topProgress = 300;
  assert(!display_changes(bottomPage, bottomStill, false).any());
  auto noBottom = bottomPage;
  noBottom.summary.bottomHalf = false;
  noBottom.resultPage = ResultPage::Score;
  assert(display_changes(bottomPage, noBottom, false).resultBody); // 点の数が変わる。
  // ②のヘッダーのドット絵は、ヘッダーの絵の範囲だけを描き直す。
  auto artPage = topPage;
  artPage.topArt = {true, true, 0};
  diff = display_changes(topPage, artPage, false);
  assert(diff.topArt && !diff.topBody && !diff.topCurve && !diff.topCell && !diff.resultBody && !diff.resultHeader);
  area = display_damage(diff, false);
  assert(area.containsPixel(TOP_ART_X, TOP_ART_Y) && area.containsPixel(TOP_ART_X + TOP_ART_WIDTH - 1, TOP_ART_Y + TOP_ART_HEIGHT - 1));
  // 16画素のタイルへ丸めた範囲（x=192〜367、y=0〜79）の外は送らない。自ノードの札・本文・ボタン枠に触れない。
  assert(area.changedTiles() == 11 * 5);
  assert(!area.containsPixel(191, 40) && !area.containsPixel(368, 40) && !area.containsPixel(260, 80));
  // 絵の範囲は、RESULT（x=24から6文字、文字サイズ3）・モード表示の消去範囲の右、自ノードの札（x=361〜）の左、区切り線（y=80）の上。
  constexpr int RESULT_LABEL_RIGHT = 24 + 6 * 6 * 3;
  assert(RESULT_LABEL_RIGHT <= TOP_ART_X && RESULT_MODE_X + RESULT_MODE_WIDTH <= TOP_ART_X);
  assert(TOP_ART_X + TOP_ART_WIDTH <= 360 && TOP_ART_Y + TOP_ART_HEIGHT <= 80);
  auto artNext = artPage;
  artNext.topArt.ms = TOP_ART_STEP_MS;
  diff = display_changes(artPage, artNext, false);
  assert(diff.topArt && !diff.topBody && !diff.topCurve && !diff.topCell && !diff.resultBody);
  assert(!display_changes(artPage, artPage, false).any()); // 何も変わらないときは描かない。
  auto artBlink = artPage;
  artBlink.previewLit = !artPage.previewLit;
  diff = display_changes(artPage, artBlink, false);
  assert(!diff.topArt && diff.topCell == 12); // マスの点滅では絵を描き直さない。
  // 見出しのモード表示は絵の範囲に重ならないので、モードが変わっても絵は描き直さない。
  auto artMode = artPage;
  artMode.mode = ControlMode::Auto;
  diff = display_changes(artPage, artMode, false);
  assert(diff.resultHeader && !diff.topArt);
  area = display_damage(diff, false);
  assert(area.containsPixel(RESULT_MODE_X, RESULT_MODE_Y) && !area.containsPixel(260, 56) && !area.containsPixel(343, 71));
  assert(display_changes(artPage, artPage, true).topArt); // 全画面を描き直すときも描く。
  // ページを替えたら絵を消す（描き直して黒にする）。
  auto artLeft = artPage;
  artLeft.resultPage = ResultPage::Bests;
  artLeft.topArt = {};
  diff = display_changes(artPage, artLeft, false);
  assert(diff.topArt && diff.resultBody && !displayed_top_art(artLeft).shown);
  assert(!display_changes(artLeft, artLeft, false).topArt);
  // ②を出さない結果・同期待ち・①と③・Finished以外では、状態が残っていても絵を出さない。
  for (int i = 0; i < 5; ++i) {
    auto hidden = artPage;
    if (i == 0) hidden.summary.topPercent = 0;
    if (i == 1) hidden.synced = false;
    if (i == 2) hidden.resultPage = ResultPage::Score;
    if (i == 3) hidden.preview = true;
    if (i == 4) hidden.phase = GamePhase::Preparing;
    assert(!displayed_top_art(hidden).shown);
  }
  // 静かな表示では、画素の変わらない段で描き直さない（下位50%のしょんぼりは羽を下ろした後は動かない）。
  auto sadPage = bottomPage;
  sadPage.topArt = {true, false, TOP_DROOP_MS};
  auto sadLater = sadPage;
  sadLater.topArt.ms = TOP_ART_CYCLE_MS - TOP_ART_STEP_MS;
  assert(!display_changes(sadPage, sadLater, false).any());
  sadLater.topArt.ms = 0;
  diff = display_changes(sadPage, sadLater, false);
  assert(diff.topArt && !diff.topBody); // 羽を上げる段では描き直す。
  // 派手な動きの間は段ごとに描き直す。
  auto burstLater = artPage;
  burstLater.topArt.ms = TOP_ART_CYCLE_MS - TOP_ART_STEP_MS;
  assert(display_changes(artPage, burstLater, false).topArt);
  // ③は本文とボタン枠を1度だけ描き、値・ページ・自ノードが変わったときだけ描き直す。
  auto bestPage = ranked;
  bestPage.summary.gameBest = 410;
  bestPage.summary.gameBestNodes = 0x05;
  bestPage.resultPage = ResultPage::Bests;
  diff = display_changes(topPage, bestPage, false);
  assert(diff.resultBody && !diff.full && !diff.topBody && !diff.resultScore && !diff.resultRank && !diff.resultAccent);
  area = display_damage(diff, false);
  assert(area.containsPixel(0, 81) && area.containsPixel(420, 300) && !area.containsPixel(0, 0) && !area.containsPixel(400, 40));
  auto bestStill = bestPage;
  bestStill.previewLit = !bestPage.previewLit;
  bestStill.finishedProgress = 300;
  bestStill.topProgress = 300;
  bestStill.pressedMask = 8;
  assert(!display_changes(bestPage, bestStill, false).any()); // 点滅・演出・押下だけでは描かない。
  auto bestMine = bestPage;
  bestMine.summary.allTimeBestMine = true;
  assert(display_changes(bestPage, bestMine, false).resultBody);
  bestMine = bestPage;
  bestMine.selfId = NodeId::P3;
  assert(display_changes(bestPage, bestMine, false).resultBody);
  auto backToScore = bestPage;
  backToScore.resultPage = ResultPage::Score;
  diff = display_changes(bestPage, backToScore, false);
  assert(diff.resultBody && diff.resultScore && diff.resultRank && diff.resultAccent && !diff.full);
  assert(BEST_ROW_Y > 80 && BEST_ROW_Y + 2 * BEST_ROW_PITCH + BEST_ROW_HEIGHT <= 240); // ボタン枠より上。
  // Finished以外では、ページの状態を使わずにそのフェーズの画面を出す（リセットで準備画面へ戻る）。
  auto resetToPrep = bestPage;
  resetToPrep.phase = GamePhase::Preparing;
  assert(display_screen(resetToPrep) == DisplayScreen::Closed);
  diff = display_changes(bestPage, resetToPrep, false);
  assert(diff.full && diff.lobbyBody && !diff.resultBody);
  resetToPrep.phase = GamePhase::Playing;
  assert(display_screen(resetToPrep) == DisplayScreen::Playing);

  // ページ送りは左から3番目のボタン。両端は「もう一度遊ぶ」の長押しに使うので、押し始めでページを送らない。
  assert(ResultPager::NEXT_BUTTON == 4 && !(ResultPager::NEXT_BUTTON & ReadyHold::END_BUTTONS));
  // 連続運転の通常ラウンドの結果: 順位は付けない（対象者でrankがnull）。両端の枠に長押しの案内、見出しの隅に残り秒数を出す。
  auto round = displayed;
  round.phase = GamePhase::Finished;
  round.pressedMask = 0;
  round.status = {345, 0, false, true};
  round.status.round = RoundKind::Normal;
  round.status.window = 30;
  round.summary = {345, 0, true, true, 1, false};
  round.finishedProgress = 1000;
  assert(finished_state(round) == FinishedState::Ready && !rank_shown(round.summary));
  assert(result_again(round) && header_window(round) == 30);
  auto roundEnter = display_changes(displayed, round, false);
  assert(roundEnter.full && roundEnter.resultBody && roundEnter.resultAgain && roundEnter.headerWindow);
  // 終了演出（スコアの数え上げと下線）は進むが、順位の欄は何も描かないので描き直さない。
  auto roundMoving = round;
  roundMoving.finishedProgress = 750;
  auto roundDiff = display_changes(round, roundMoving, false);
  assert(roundDiff.resultScore && roundDiff.resultAccent && !roundDiff.resultRank && !roundDiff.resultAgain);
  // 順位を付ける結果（一斉ゲーム方式の対象者）と対象外は、従来どおり順位の欄を描く。
  auto rankedRound = round;
  rankedRound.summary.rank = 3;
  rankedRound.status.window = 0;
  assert(rank_shown(rankedRound.summary) && !result_again(rankedRound) && header_window(rankedRound) == -1);
  rankedRound.finishedProgress = 750;
  roundMoving = rankedRound;
  roundMoving.finishedProgress = 1000;
  assert(display_changes(rankedRound, roundMoving, false).resultRank);
  auto unrankedRound = round;
  unrankedRound.summary.eligible = false;
  assert(rank_shown(unrankedRound.summary));
  // 残り秒数は1秒ごとに見出しの隅だけを描き直す。10秒以下は注意色なので、秒数の変化でも描き直す。
  auto roundTick = round;
  roundTick.status.window = 29;
  roundDiff = display_changes(round, roundTick, false);
  assert(roundDiff.headerWindow && !roundDiff.resultBody && !roundDiff.resultAgain && !roundDiff.full && !roundDiff.resultHeader);
  auto roundTickArea = display_damage(roundDiff, false);
  assert(roundTickArea.containsPixel(HEADER_WINDOW_X, HEADER_WINDOW_Y) && !roundTickArea.containsPixel(0, 81) && !roundTickArea.containsPixel(24, 56));
  roundTick.status.window = 10;
  assert(display_changes(round, roundTick, false).headerWindow && header_window(roundTick) == 10);
  // 長押し・確認待ちの進捗は、両端の枠だけを描き直す。内側の枠（ページ送り）と本文は触れない。
  auto holdRound = round;
  holdRound.pressedMask = 9;
  holdRound.readyHoldMs = 1000;
  roundDiff = display_changes(round, holdRound, false);
  assert(roundDiff.resultAgain && !roundDiff.resultBody && !roundDiff.headerWindow && !roundDiff.full);
  auto againArea = display_damage(roundDiff, false);
  assert(againArea.containsPixel(60, 300) && againArea.containsPixel(420, 300));
  assert(!againArea.containsPixel(300, 300) && !againArea.containsPixel(180, 300) && !againArea.containsPixel(24, 128));
  holdRound.readyPending = true;
  holdRound.readyHoldMs = ReadyHold::DURATION_MS;
  assert(display_changes(round, holdRound, false).resultAgain);
  auto noHold = round;
  noHold.pressedMask = 2; // 内側のボタンを押しても、案内は描き直さない（ページ送りは左から3番目）。
  assert(!display_changes(round, noHold, false).any());
  noHold.pressedMask = ResultPager::NEXT_BUTTON;
  assert(!display_changes(round, noHold, false).any());
  // ページ送りの案内は左から3番目の枠、「もう一度遊ぶ」は両端の枠、左から2番目の枠は空にする。
  assert(button_frame_x(2) == 241 && button_frame_x(1) == 121);
  auto pageRound = round;
  pageRound.summary.topPercent = 12;
  pageRound.resultPage = ResultPage::TopPercent;
  roundDiff = display_changes(round, pageRound, false);
  assert(roundDiff.resultBody && roundDiff.topBody && roundDiff.resultAgain && !roundDiff.full); // ページが変わると枠も描き直す。
  assert(display_damage(roundDiff, false).containsPixel(300, 300) && display_damage(roundDiff, false).containsPixel(60, 300));
  // 時間制限のない運転方式（window=0）とボット固定では、長押しの案内も残り秒数も出さない。
  auto oneShot = round;
  oneShot.status.window = 0;
  assert(!result_again(oneShot) && header_window(oneShot) == -1);
  roundDiff = display_changes(round, oneShot, false);
  assert(roundDiff.headerWindow && roundDiff.resultAgain && !roundDiff.resultBody); // 出ていた案内を消す。
  auto autoRound = round;
  autoRound.mode = ControlMode::Auto;
  assert(!result_again(autoRound) && header_window(autoRound) == 30);
  assert(display_changes(round, autoRound, false).resultAgain);
  // 結果を表示できる状態だけに出す。取得中は案内も残り秒数も出さない。
  auto notReady = round;
  notReady.summary.known = false;
  assert(finished_state(notReady) == FinishedState::Result && !result_again(notReady) && header_window(notReady) == -1);
  notReady = round;
  notReady.synced = false;
  assert(finished_state(notReady) == FinishedState::Syncing && !result_again(notReady) && header_window(notReady) == -1);
  // 一斉ゲーム方式の練習の終了（status.roundがtutorial）は、summaryが届かないので結果を出さない。
  auto practiceEnd = round;
  practiceEnd.summary = {};
  practiceEnd.status.round = RoundKind::Tutorial;
  practiceEnd.status.window = 0;
  assert(finished_state(practiceEnd) == FinishedState::PracticeEnded && !result_again(practiceEnd));
  assert(display_screen(practiceEnd) == DisplayScreen::Finished && header_window(practiceEnd) == -1);
  roundDiff = display_changes(round, practiceEnd, false);
  assert(roundDiff.resultBody && !roundDiff.full);
  practiceEnd.status.known = false;
  assert(finished_state(practiceEnd) == FinishedState::Status);

  auto preview = result;
  preview.preview = true;
  assert(display_screen(preview) == DisplayScreen::Preview);
  assert(display_changes(result, preview, false).full);
  auto dark = preview;
  dark.previewLit = false;
  diff = display_changes(preview, dark, false);
  assert(diff.previewBody && !diff.resultBody && !diff.full);
  const auto beforeScore = dark;
  dark.summary.score = 10;
  assert(!display_changes(beforeScore, dark, false).any());

  // カウントダウンは届いた値ごとに本文だけを描き直し、GO!の後で通常のPlaying画面へ戻る。
  auto counting = displayed;
  counting.status.countdown = 3;
  assert(display_screen(counting) == DisplayScreen::Countdown);
  auto countdown = display_changes(displayed, counting, false);
  assert(countdown.full && countdown.self && countdown.countdownBody && countdown.cards.none());
  assert(countdown.buttons == 0 && !countdown.label && !countdown.score && !countdown.remaining);
  auto two = counting;
  two.status.countdown = 2;
  countdown = display_changes(counting, two, false);
  assert(countdown.countdownBody && !countdown.full && !countdown.self);
  const auto countdownArea = display_damage(countdown, false);
  assert(countdownArea.containsPixel(78, 88) && countdownArea.containsPixel(401, 231));
  assert(!countdownArea.containsPixel(0, 0) && !countdownArea.containsPixel(400, 40));
  auto ignored = two;
  ignored.pressedMask = 5;
  ignored.status.score = 50;
  ignored.cards[0].y = 30;
  assert(!display_changes(two, ignored, false).any()); // ボタン・スコア・カードは描かない。
  auto other = two;
  other.selfId = NodeId::P8;
  countdown = display_changes(two, other, false);
  assert(countdown.self && countdown.countdownBody && !countdown.full); // 数字の色も変わる。
  auto go = two;
  go.status.countdown = 0;
  go.countdownGo = true;
  assert(display_screen(go) == DisplayScreen::Countdown);
  countdown = display_changes(two, go, false);
  assert(countdown.countdownBody && !countdown.full);
  auto playing = go;
  playing.countdownGo = false;
  assert(display_screen(playing) == DisplayScreen::Playing);
  countdown = display_changes(go, playing, false);
  assert(countdown.full && countdown.cards.all() && countdown.buttons == 15 && countdown.score);
  // 同期・STATUS取得待ちと確認表示では数字を出さない。
  for (int i = 0; i < 6; ++i) {
    auto hidden = counting;
    if (i == 0) hidden.synced = false;
    if (i == 1) hidden.awaitingStatus = true;
    if (i == 2) hidden.status.known = false;
    if (i == 3) hidden.selfId = NodeId::None;
    if (i == 4) hidden.phase = GamePhase::Preparing;
    if (i == 5) hidden.preview = true;
    assert(!countdown_enabled(hidden) && display_screen(hidden) != DisplayScreen::Countdown);
  }
  CountdownAnimation goAnimation;
  assert(!goAnimation.update(0, true, 0)); // 0のままではGO!を出さない。
  assert(!goAnimation.update(1, true, UINT32_MAX - 100));
  assert(goAnimation.update(0, true, UINT32_MAX - 100));
  assert(goAnimation.update(0, true, CountdownAnimation::GO_MS - 102)); // millis()の周回をまたぐ。
  assert(!goAnimation.update(0, true, CountdownAnimation::GO_MS - 101));
  assert(!goAnimation.update(0, true, 2000)); // 1回の0への変化で1度だけ出す。
  assert(!goAnimation.update(2, true, 3000));
  assert(!goAnimation.update(0, false, 3001)); // 数字を出せない間に0になった場合は出さない。
  assert(!goAnimation.update(0, true, 3002));
  assert(!goAnimation.update(1, true, 4000));
  assert(goAnimation.update(0, true, 4001));
  assert(!goAnimation.update(0, false, 4002)); // Reset・Finish・再同期などで取り消す。
  assert(!goAnimation.update(0, true, 4003));

  // 獲得点は経過時間で進み、約1.5秒で消える。列ごとに1件で、新しい点は最も古い列を上書きする。
  const auto batch = [](std::initializer_list<uint8_t> values) {
    GainBatch gains;
    for (uint8_t value : values) gains.push(value);
    return gains;
  };
  GainAnimation gainAnimation;
  gainAnimation.update(batch({15, 8}), true, 16);
  auto floats = gainAnimation.snapshot();
  assert(floats[0].active && floats[0].value == 15 && floats[0].ageMs == 0);
  assert(floats[1].active && floats[1].value == 8 && floats[1].ageMs == 0 && !floats[2].active);
  gainAnimation.update({}, true, 700);
  gainAnimation.update(batch({10}), true, 100);
  floats = gainAnimation.snapshot();
  assert(floats[0].ageMs == 800 && floats[1].ageMs == 800 && floats[2].value == 10 && floats[2].ageMs == 0);
  gainAnimation.update({}, true, GAIN_DURATION_MS - 801);
  floats = gainAnimation.snapshot();
  assert(floats[0].active && floats[0].ageMs == GAIN_DURATION_MS - 1 && floats[2].active);
  gainAnimation.update({}, true, 1);
  floats = gainAnimation.snapshot();
  assert(!floats[0].active && !floats[1].active && floats[2].active && floats[2].ageMs == GAIN_DURATION_MS - 800);
  // 同時に流すのは最新の3件まで。次の列は0から始まり、古い点の列を順に上書きする。
  gainAnimation.update(batch({1, 2, 3, 4, 5}), true, 0);
  floats = gainAnimation.snapshot();
  assert(floats[0].value == 4 && floats[1].value == 5 && floats[2].value == 3);
  for (const auto& gain : floats) assert(gain.active && gain.ageMs == 0);
  gainAnimation.update(batch({8}), false, 16); // 流せない間の点は捨て、流れている点も消す。
  for (const auto& gain : gainAnimation.snapshot()) assert(!gain.active);
  gainAnimation.update({}, true, 16);
  for (const auto& gain : gainAnimation.snapshot()) assert(!gain.active);
  gainAnimation.update({}, true, UINT32_MAX); // 長い間隔でも桁あふれしない。
  for (const auto& gain : gainAnimation.snapshot()) assert(!gain.active);
  // 下から上へ等速で上がり、最後の0.5秒で暗くなる。消える直前も真っ黒にはしない。
  assert(gain_offset({15, 0, true}) == 0 && gain_offset({15, GAIN_DURATION_MS - 1, true}) == GAIN_RISE - 1);
  assert(gain_level({15, 0, true}) == GAIN_LEVELS && gain_level({15, GAIN_DURATION_MS - GAIN_FADE_MS, true}) == GAIN_LEVELS);
  assert(gain_level({15, GAIN_DURATION_MS - GAIN_FADE_MS + 1, true}) == GAIN_LEVELS);
  assert(gain_level({15, GAIN_DURATION_MS - 1, true}) == 1);
  for (uint16_t age = 1; age < GAIN_DURATION_MS; ++age)
    assert(gain_level({15, age, true}) <= gain_level({15, uint16_t(age - 1), true}));
  assert(dim565(0xffff, GAIN_LEVELS) == 0xffff && dim565(0xffff, 0) == 0 && dim565(0xf800, 4) == 0x7800);
  assert(dim565(0x07e0, 4) == 0x03e0 && dim565(0x001f, 4) == 0x000f);
  // 帯・各列・順位の枠は情報欄の中に収まり、他の行と重ならない。
  assert(GAIN_LANE_Y > 81 + INFO_HEADER_HEIGHT - 1 && GAIN_LANE_Y + GAIN_LANE_HEIGHT <= INFO_SCORE_Y);
  assert(GAIN_START_Y - GAIN_RISE + GAIN_TEXT_HEIGHT > GAIN_LANE_Y); // 消える直前も文字の下側が帯に残る。
  assert(GAIN_COLUMN_X >= INFO_X && GAIN_COLUMN_X + 2 * GAIN_COLUMN_PITCH + 36 <= INFO_X + INFO_WIDTH);
  assert(INFO_SCORE_Y + 24 <= INFO_READY_Y && INFO_READY_Y + 8 <= INFO_REMAINING_Y);
  assert(INFO_REMAINING_Y + 24 <= INFO_RANK_Y && INFO_RANK_Y + INFO_RANK_HEIGHT <= 240);
  assert(INFO_RANK_X >= 366 + 4 * 6 && INFO_RANK_X + INFO_RANK_WIDTH <= INFO_X + INFO_WIDTH); // 「RANK」の右。

  // 順位が上がったときの緑の点滅は、RANK UPの演出と一緒にtests/playing_effects.cppで検証する。

  // 獲得点・順位の変化は情報欄の帯・順位の枠だけを描き直す。
  DisplaySnapshot info{};
  info.selfId = NodeId::P3;
  info.phase = GamePhase::Playing;
  info.mode = ControlMode::Manual;
  info.synced = true;
  info.status = {40, 80, true, true, 0, 3};
  assert(gains_enabled(info));
  auto gained = info;
  gained.gains[1] = {15, 0, true};
  auto infoChanges = display_changes(info, gained, false);
  assert(infoChanges.gains && !infoChanges.score && !infoChanges.rank && infoChanges.cards.none() && !infoChanges.label);
  auto infoDamage = display_damage(infoChanges, false);
  assert(infoDamage.containsPixel(INFO_X, GAIN_LANE_Y) && infoDamage.containsPixel(478, GAIN_LANE_Y + GAIN_LANE_HEIGHT - 1));
  assert(!infoDamage.containsPixel(351, GAIN_LANE_Y) && !infoDamage.containsPixel(400, 200) && infoDamage.changedTiles() <= 24);
  auto older = gained;
  older.gains[1].ageMs = 10; // 画素が同じなら描き直さない。
  assert(!display_changes(gained, older, false).any());
  older.gains[1].ageMs = GAIN_DURATION_MS / GAIN_RISE;
  assert(display_changes(gained, older, false).gains);
  older = gained;
  older.gains[1].value = 8; // 色が変わる。
  assert(display_changes(gained, older, false).gains);
  older = gained;
  older.selfId = NodeId::P4;
  assert(display_changes(gained, older, false).gains);
  auto rankUp = info;
  rankUp.status.rank = 2;
  infoChanges = display_changes(info, rankUp, false);
  assert(infoChanges.rank && !infoChanges.gains && !infoChanges.score && !infoChanges.remaining);
  infoDamage = display_damage(infoChanges, false);
  assert(infoDamage.containsPixel(INFO_RANK_X, INFO_RANK_Y) && infoDamage.containsPixel(477, 238));
  assert(!infoDamage.containsPixel(400, 150) && !infoDamage.containsPixel(300, 220));
  auto flashing = rankUp;
  flashing.rankFlash = true;
  assert(display_changes(rankUp, flashing, false).rank && !display_changes(rankUp, flashing, false).gains);
  assert(display_changes(flashing, rankUp, false).rank);
  flashing.status.rank = 0; // nullの「--」は光らせない。
  rankUp.status.rank = 0;
  assert(!display_changes(rankUp, flashing, false).any());
  // スコアは獲得点を足さず、statusの値をそのまま描き直す。
  auto scored = gained;
  scored.status.score = 55;
  infoChanges = display_changes(gained, scored, false);
  assert(infoChanges.score && !infoChanges.gains);
  // カウントダウン・GO!・確認表示・同期待ち・STATUS待ちでは流さず、順位も光らせない。
  for (int i = 0; i < 6; ++i) {
    auto hidden = gained;
    hidden.rankFlash = true;
    if (i == 0) hidden.status.countdown = 2;
    if (i == 1) hidden.countdownGo = true;
    if (i == 2) hidden.preview = true;
    if (i == 3) hidden.synced = false;
    if (i == 4) hidden.awaitingStatus = true;
    if (i == 5) hidden.status.known = false;
    assert(!gains_enabled(hidden) && !displayed_rank_flash(hidden));
    for (const auto& gain : displayed_gains(hidden)) assert(!gain.active);
  }
  // Finished・Preparingでも流さない。
  auto finishedInfo = gained;
  finishedInfo.phase = GamePhase::Finished;
  assert(!gains_enabled(finishedInfo));
  finishedInfo.phase = GamePhase::Preparing;
  assert(!gains_enabled(finishedInfo));
  // STATUS待ちへ移ると帯を空に描き直し、戻ると描く。
  auto waiting = gained;
  waiting.awaitingStatus = true;
  assert(display_changes(gained, waiting, false).gains && display_changes(waiting, gained, false).gains);

  // 炎・マスコット・コンボの演出は通常のPlaying画面だけで出し、左の列とカード欄の右の余白だけを描き直す。
  PlayingEffects effects;
  uint32_t now = 100;
  for (; now < 2000; now += 16) effects.update(12, 3, true, now);
  auto lively = info;
  lively.status.combo = 12;
  lively.effects = effects.frame();
  assert(displayed_effects(lively) == effects.frame() && displayed_combo(lively) == 12);
  assert(displayed_crown(lively) == Crown::Bronze && lively.effects.flameLit && lively.effects.form == MascotForm::Pair);
  assert(display_changes(info, lively, true).leftColumn && display_changes(info, lively, true).rightMargin);
  auto stepped = lively;
  effects.update(12, 3, true, now + 1); // 同じ1段の間の発行では状態が変わらない。
  stepped.effects = effects.frame();
  assert(!display_changes(lively, stepped, false).any());
  effects.update(12, 3, true, now += FLAME_STEP_MS);
  stepped.effects = effects.frame();
  auto fx = display_changes(lively, stepped, false);
  assert(fx.leftColumn && fx.rightMargin && !fx.full && fx.cards.none() && !fx.rank && !fx.gains && !fx.score && !fx.self);
  auto fxDamage = display_damage(fx, false);
  assert(fxDamage.containsPixel(1, 0) && fxDamage.containsPixel(119, 239) && fxDamage.containsPixel(320, 0) &&
         fxDamage.containsPixel(359, 239));
  assert(!fxDamage.containsPixel(200, 100) && !fxDamage.containsPixel(400, 100) && !fxDamage.containsPixel(10, 260));
  // 何も変わらないときは差分が出ない。
  assert(!display_changes(stepped, stepped, false).any());
  // コンボ数・冠だけが変わったら左の列だけ。冠が変わらない順位の変化では左の列を描き直さない。
  auto counted = stepped;
  counted.status.combo = 13;
  fx = display_changes(stepped, counted, false);
  assert(fx.leftColumn && !fx.rightMargin);
  auto crowned = stepped;
  crowned.status.rank = 1;
  fx = display_changes(stepped, crowned, false);
  assert(fx.leftColumn && fx.rank && !fx.rightMargin);
  auto lower = stepped;
  lower.status.rank = 5;
  auto lowest = stepped;
  lowest.status.rank = 6;
  fx = display_changes(lower, lowest, false);
  assert(!fx.leftColumn && fx.rank);
  // コンボ数は3以上でだけ出す。マスコットがいなければ冠もない。
  auto small = info;
  small.status.combo = 2;
  small.status.rank = 1;
  PlayingEffects quiet;
  quiet.update(2, 1, true, 0);
  quiet.update(2, 1, true, 1000);
  small.effects = quiet.frame();
  assert(displayed_combo(small) == 0 && displayed_crown(small) == Crown::None && small.effects.flameLit);
  auto smallRanked = small;
  smallRanked.status.rank = 2;
  assert(!display_changes(small, smallRanked, false).leftColumn);
  // Playing以外（カウントダウン・GO!・確認表示・同期待ち・STATUS待ち・未取得・Preparing・Finished）では出さない。
  for (int i = 0; i < 8; ++i) {
    auto hidden = stepped;
    if (i == 0) hidden.status.countdown = 2;
    if (i == 1) hidden.countdownGo = true;
    if (i == 2) hidden.preview = true;
    if (i == 3) hidden.synced = false;
    if (i == 4) hidden.awaitingStatus = true;
    if (i == 5) hidden.status.known = false;
    if (i == 6) hidden.phase = GamePhase::Preparing;
    if (i == 7) hidden.phase = GamePhase::Finished;
    assert(displayed_effects(hidden) == EffectsFrame{} && displayed_combo(hidden) == 0);
    assert(displayed_crown(hidden) == Crown::None);
  }
  // カウントダウン画面へ切り替わると演出を消す。戻ったときは空の状態から描き直す。
  auto counting2 = stepped;
  counting2.status.countdown = 3;
  fx = display_changes(stepped, counting2, false);
  assert(fx.full && fx.countdownBody && !fx.leftColumn && !fx.rightMargin);
  effects.update(12, 3, false, now + 16);
  assert(!effects.running() && effects.frame() == EffectsFrame{});
  counting2.effects = effects.frame();
  auto back = counting2;
  back.status.countdown = 0;
  fx = display_changes(counting2, back, false);
  assert(fx.full && fx.leftColumn && fx.rightMargin && displayed_effects(back) == EffectsFrame{});
  effects.update(12, 3, true, now + 32); // 戻った直後は段階の姿を下端から出し、炎は根元から育てる。
  assert(effects.frame().flightMs == 0 && effects.frame().flameStep == 0 && effects.frame().milestoneMs == EFFECT_OFF);
  // STATUS待ちへ移ると左の列を空に描き直す。
  auto waitingFx = stepped;
  waitingFx.awaitingStatus = true;
  assert(display_changes(stepped, waitingFx, false).leftColumn && display_changes(stepped, waitingFx, false).rightMargin);
  static_assert(std::is_trivially_copyable<EffectsFrame>::value, "effects travel inside the snapshot");

  ScanTiming timing;
  timing.locked = true;
  std::array<ScanTransferBlock, 3> blocks{{{112, 16, 0, 500}, {304, 16, 400, 1200}, {464, 16, 800, 2000}}};
  auto plan = plan_transfer(timing, blocks, 3);
  assert(plan.feasible);
  for (const auto& block : blocks) {
    const float firstScan = (block.x - 112) * timing.periodUs / SCAN_LINES;
    const float lastScan = (block.x + block.width - 1 - 112) * timing.periodUs / SCAN_LINES;
    assert(plan.startDelayFromScanUs + block.earliestStartUs >= lastScan + plan.guardUs + plan.blankingUs - 0.01f);
    assert(plan.startDelayFromScanUs + block.latestEndUs <= firstScan + timing.periodUs - plan.guardUs + 0.01f);
  }
  blocks[2].latestEndUs = 50000;
  plan = plan_transfer(timing, blocks, 3);
  assert(!plan.feasible && std::isfinite(plan.startDelayFromScanUs));
  assert(!plan_transfer(timing, blocks, 0).feasible);
  std::puts("PASS: rank and gains in the info column, playing effects only on the normal Playing screen with left-column and right-margin damage, state-driven damage, old/new card rows, clipping, no-op frames, countdown screen, practice hint bands and bot load bars, top percent page with header art damage, three-page pager and best scores, sparse transfer timing");
}
