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
  assert(area.containsPixel(24, 128) && area.containsPixel(359, 319) && !area.containsPixel(0, 0));
  assert(!area.containsPixel(420, 300)); // 演出中も右端の枠（ページ送りの案内）は描かない。
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
  assert(display_screen(resetToPrep) == DisplayScreen::Preparing);
  diff = display_changes(bestPage, resetToPrep, false);
  assert(diff.full && diff.preparingBody && !diff.resultBody);
  resetToPrep.phase = GamePhase::Playing;
  assert(display_screen(resetToPrep) == DisplayScreen::Playing);

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
  std::puts("PASS: rank and gains in the info column, playing effects only on the normal Playing screen with left-column and right-margin damage, state-driven damage, old/new card rows, clipping, no-op frames, countdown screen, top percent page, three-page pager and best scores, sparse transfer timing");
}
