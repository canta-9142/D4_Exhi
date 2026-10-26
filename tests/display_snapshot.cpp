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
  std::puts("PASS: state-driven damage, old/new card rows, clipping, no-op frames, sparse transfer timing");
}
