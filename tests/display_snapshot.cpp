#include <cassert>
#include <cstdio>
#include <cmath>
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

  next = displayed;
  next.phase = GamePhase::Preparing;
  next.status.ready = false;
  auto prep = display_changes(displayed, next, false);
  assert(prep.full && prep.cards.none() && prep.buttons == 0 && prep.preparingBody);
  assert(preparing_state(next) == PreparingState::Manual);
  auto holding = next;
  holding.readyHoldMs = 1000;
  prep = display_changes(next, holding, false);
  assert(prep.preparingBar && !prep.full && !prep.preparingBody && !prep.cards.any());
  assert(display_damage(prep, false).changedTiles() < 100);
  holding.readyPending = true;
  assert(preparing_state(holding) == PreparingState::Confirming);
  holding.status.ready = true;
  assert(preparing_state(holding) == PreparingState::Ready);
  holding.mode = ControlMode::Auto;
  assert(preparing_state(holding) == PreparingState::Auto);
  holding.synced = false;
  assert(preparing_state(holding) == PreparingState::Waiting);
  assert(display_changes(next, displayed, false).full);


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
  assert(area.containsPixel(24, 128) && area.containsPixel(455, 319) && !area.containsPixel(0, 0));
  moving.summary.eligible = false;
  moving.summary.rank = 0;
  assert(display_changes(result, moving, false).resultRank);
  moving.synced = false;
  assert(finished_state(moving) == FinishedState::Syncing);
  assert(display_changes(result, moving, false).resultBody);
  assert(animation.update(result.summary, false, 3000) == 1000);
  assert(animation.update(result.summary, true, 3001) == 1000);
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
  std::puts("PASS: state-driven damage, old/new card rows, clipping, no-op frames, countdown screen, sparse transfer timing");
}
