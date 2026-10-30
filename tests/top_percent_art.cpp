#include <algorithm>
#include <cassert>
#include <cstdio>
#include <vector>
#include "../src/player/top_percent_art.hpp"

// fillRectを記録する描画先。
struct Recorder {
  struct Rect { int x, y, w, h; uint16_t color; };
  std::vector<Rect> rects;
  void fillRect(int x, int y, int w, int h, uint16_t color) { rects.push_back({x, y, w, h, color}); }
  bool inside() const {
    return std::all_of(rects.begin(), rects.end(), [](const Rect& r) {
      return r.w > 0 && r.h > 0 && r.x >= TOP_ART_X && r.y >= TOP_ART_Y &&
             r.x + r.w <= TOP_ART_X + TOP_ART_WIDTH && r.y + r.h <= TOP_ART_Y + TOP_ART_HEIGHT;
    });
  }
};

static FinalSummary top(uint8_t percent) {
  FinalSummary s;
  s.known = true;
  s.eligible = true;
  s.rank = 1;
  s.revision = 1;
  s.animate = true;
  s.topPercent = percent;
  return s;
}

static void test_tiers() {
  // 段階の境界（1・2、5・6、10・11、25・26、50）。
  const std::pair<uint8_t, TopProp> props[] = {
    {1, TopProp::Crown}, {2, TopProp::Trophy}, {5, TopProp::Trophy}, {6, TopProp::Star}, {10, TopProp::Star},
    {11, TopProp::Medal}, {25, TopProp::Medal}, {26, TopProp::None}, {50, TopProp::None},
  };
  for (const auto& entry : props) {
    const TopArtTier* tier = top_art_tier(top(entry.first));
    assert(tier && tier->prop == entry.second && tier->face == MascotFace::Joy);
  }
  assert(top_art_tier(top(1))->motion == TopMotion::Hop && top_art_tier(top(1))->burst == TopBurst::Fireworks);
  assert(top_art_tier(top(5))->burst == TopBurst::StreakStars && top_art_tier(top(6))->burst == TopBurst::Confetti);
  assert(top_art_tier(top(10))->motion == TopMotion::SpinHop && top_art_tier(top(11))->motion == TopMotion::Swing);
  // 26〜50は汎用の獲得演出（喜びの顔で羽ばたくマスコットと紙吹雪）。
  assert(top_art_tier(top(26))->motion == TopMotion::Flap && top_art_tier(top(50))->burst == TopBurst::Confetti);
  assert(top_art_tier(top(25)) != top_art_tier(top(26)) && top_art_tier(top(26)) == top_art_tier(top(50)));
  // 表は区切りの昇順で、最後の区切りが50。
  constexpr size_t count = sizeof(TOP_ART_TIERS) / sizeof(TOP_ART_TIERS[0]);
  for (size_t i = 1; i < count; ++i) assert(TOP_ART_TIERS[i - 1].maxPercent < TOP_ART_TIERS[i].maxPercent);
  assert(TOP_ART_TIERS[count - 1].maxPercent == 50);
  // nullで下位50%でもない（対象外・手動の終了・記録を使えない起動）なら出さない。
  assert(!top_art_tier(top(0)));
  FinalSummary unranked = top(0);
  unranked.eligible = false;
  unranked.rank = 0;
  assert(!top_art_tier(unranked));
  // 下位50%は値を伏せたまま、全員同じしょんぼりの絵。祝う動きはない。
  FinalSummary bottom = top(0);
  bottom.bottomHalf = true;
  const TopArtTier* sad = top_art_tier(bottom);
  assert(sad == &TOP_ART_BOTTOM && sad->face == MascotFace::Sad && sad->motion == TopMotion::Droop);
  assert(sad->burst == TopBurst::None && sad->prop == TopProp::None);
}

static void test_timeline() {
  // 初回の通常遷移の②: 塗りの演出の間は出さず、塗り終えてから派手な動きを1周、その後は絵だけを静かに繰り返す。
  TopPercentArt art;
  art.update(true, true, 0, 1000);
  assert(!art.frame().shown);
  art.update(true, true, 999, 1999);
  assert(!art.frame().shown);
  art.update(true, true, 1000, 2003);
  assert(art.frame().shown && art.frame().burst && art.frame().ms == 0);
  art.update(true, true, 1000, 2003 + TOP_ART_STEP_MS - 1);
  assert(art.frame().ms == 0); // 段の途中では進めない。
  art.update(true, true, 1000, 2003 + TOP_ART_STEP_MS);
  assert(art.frame().ms == TOP_ART_STEP_MS);
  art.update(true, true, 1000, 2003 + TOP_ART_CYCLE_MS - 1);
  assert(art.frame().burst && art.frame().ms == TOP_ART_CYCLE_MS - TOP_ART_STEP_MS);
  art.update(true, true, 1000, 2003 + TOP_ART_CYCLE_MS);
  assert(art.frame().shown && !art.frame().burst && art.frame().ms == 0); // 1周で終える。
  art.update(true, true, 1000, 2003 + 5 * TOP_ART_CYCLE_MS + 120);
  assert(art.frame().shown && !art.frame().burst && art.frame().ms == 100); // 絵は繰り返す。
  // ページを替えたら止め、②に戻ったら静かな表示からすぐに始める。
  art.update(false, false, 1000, 20000);
  assert(!art.frame().shown && !art.frame().burst);
  art.update(true, false, 1000, 20010);
  assert(art.frame().shown && !art.frame().burst && art.frame().ms == 0);
  // 派手な動きの途中で取り消されたら（celebratingが偽）、静かな表示へ移る。
  TopPercentArt cancelled;
  cancelled.update(true, true, 1000, 0);
  assert(cancelled.frame().burst);
  cancelled.update(true, false, 1000, 100);
  assert(cancelled.frame().shown && !cancelled.frame().burst && cancelled.frame().ms == 100);
  // 静かな②は塗りの進行度によらず、開いたときから出す。
  TopPercentArt quiet;
  quiet.update(true, false, 0, 500);
  assert(quiet.frame().shown && !quiet.frame().burst && quiet.frame().ms == 0);

  // 時刻だけで決まり、発行の間隔に左右されない。millis()の周回をまたいでも進む。
  TopPercentArt fine, coarse;
  const uint32_t start = UINT32_MAX - 700;
  fine.update(true, true, 1000, start);
  coarse.update(true, true, 1000, start);
  for (uint32_t elapsed = 16; elapsed <= 3000; elapsed += 16) fine.update(true, true, 1000, start + elapsed);
  for (uint32_t elapsed = 37; elapsed <= 3000; elapsed += 37) coarse.update(true, true, 1000, start + elapsed);
  fine.update(true, true, 1000, start + 3000);
  coarse.update(true, true, 1000, start + 3000);
  assert(fine.frame().ms == coarse.frame().ms && fine.frame().burst == coarse.frame().burst);
  assert(fine.frame().ms == 1000 && !fine.frame().burst);
}

static void test_pager() {
  // ResultPagerと組み合わせ、派手な動きは通常の終了遷移で初めて開いた②だけに出す。
  ResultPager pager;
  TopPercentArt art;
  FinalSummary s = top(3);
  auto step = [&](bool next, uint32_t now, bool ready = true) {
    pager.update(s, true, ready, next, now);
    art.update(ready && pager.page() == ResultPage::TopPercent && top_art_tier(s), pager.celebrating(), pager.progress(), now);
    return art.frame();
  };
  assert(!step(false, 0).shown); // ①
  TopArtFrame f = step(true, 100); // ②を開き、塗りの演出が始まる。
  assert(pager.page() == ResultPage::TopPercent && pager.celebrating() && !f.shown);
  assert(!step(false, 600).shown);
  f = step(false, 100 + ResultPager::INTRO_MS);
  assert(f.shown && f.burst && f.ms == 0 && pager.celebrating());
  f = step(false, 100 + ResultPager::INTRO_MS + 1000);
  assert(f.burst && f.ms == 1000);
  f = step(false, 100 + ResultPager::INTRO_MS + TOP_ART_CYCLE_MS);
  assert(f.shown && !f.burst);
  f = step(true, 4000); // ③
  assert(!f.shown && !pager.celebrating());
  step(true, 4100); // ①
  f = step(true, 4200); // 2回目の②は静かな表示からすぐに出す。
  assert(f.shown && !f.burst && f.ms == 0 && !pager.celebrating());
  // 派手な動きの途中でページを替えても、戻ったら静かな表示。
  s.revision = 2;
  step(false, 5000);
  step(true, 5100);
  f = step(false, 5100 + ResultPager::INTRO_MS + 500);
  assert(f.burst);
  step(true, 6700);
  step(true, 6800);
  f = step(true, 6900);
  assert(pager.page() == ResultPage::TopPercent && f.shown && !f.burst);
  // 塗りの演出中の再同期では取り消し、復旧後は静かな表示。
  s.revision = 3;
  step(false, 7000);
  assert(!step(true, 7100).shown);
  assert(!step(false, 7200, false).shown);
  f = step(false, 7300);
  assert(f.shown && !f.burst && f.ms == 0 && !pager.celebrating());
  // 再同期・初期同期・再接続で取得した結果（animate=false）は、初めて開いても静かな表示。
  s.revision = 4;
  s.animate = false;
  step(false, 8000);
  f = step(true, 8100);
  assert(f.shown && !f.burst && f.ms == 0);
  // 下位50%は塗りの演出がないので、開いたときからしょんぼりの絵を静かに出す。
  s.revision = 5;
  s.animate = true;
  s.topPercent = 0;
  s.bottomHalf = true;
  step(false, 9000);
  f = step(true, 9100);
  assert(f.shown && !f.burst && f.ms == 0);
  // ②を出さない結果では、②を飛ばすので絵も出さない。
  s.revision = 6;
  s.bottomHalf = false;
  step(false, 10000);
  f = step(true, 10100);
  assert(pager.page() == ResultPage::Bests && !f.shown);
}

static void test_inside_area() {
  // すべての段階と時刻で、マスコットと小物は切り取らずに範囲に収まり、派手な動きを重ねても範囲の外に描かない。
  std::vector<const TopArtTier*> tiers;
  for (const auto& tier : TOP_ART_TIERS) tiers.push_back(&tier);
  tiers.push_back(&TOP_ART_BOTTOM);
  for (const TopArtTier* tier : tiers) {
    bool burstDrawn = false;
    for (uint16_t ms = 0; ms < TOP_ART_CYCLE_MS; ms += 10) {
      Recorder figure;
      draw_top_art_figure(figure, *tier, top_art_figure(*tier, ms), ms);
      assert(!figure.rects.empty() && figure.inside());
      Recorder quiet, loud;
      draw_top_art(quiet, *tier, {true, false, ms});
      draw_top_art(loud, *tier, {true, true, ms});
      assert(quiet.inside() && loud.inside());
      assert(quiet.rects.size() == figure.rects.size()); // 静かな表示は絵だけ。
      burstDrawn |= loud.rects.size() > quiet.rects.size();
    }
    assert(burstDrawn == (tier->burst != TopBurst::None));
    Recorder hidden;
    draw_top_art(hidden, *tier, {});
    assert(hidden.rects.empty());
  }
}

static void test_motions() {
  const TopArtTier& crown = *top_art_tier(top(1));
  const TopArtTier& trophy = *top_art_tier(top(3));
  const TopArtTier& star = *top_art_tier(top(8));
  const TopArtTier& medal = *top_art_tier(top(20));
  const TopArtTier& flap = *top_art_tier(top(40));
  // 1以下: 王冠をかぶり、1周に2回跳ねる。着地している間は基準の高さ（羽を下ろすコマは1画素上げる）。
  int highest = 1000;
  for (uint16_t ms = 0; ms < TOP_ART_CYCLE_MS; ms += TOP_ART_STEP_MS) {
    const TopArtFigure f = top_art_figure(crown, ms);
    assert(f.pose.crown == Crown::Gold && f.pose.face == MascotFace::Joy && !f.flip);
    highest = std::min<int>(highest, f.y);
    if (ms % TOP_HOP_MS >= TOP_HOP_AIR_MS)
      assert(f.y == top_art_rest_y(crown) - (f.pose.wing == MASCOT_WING_DOWN_FRAME ? TOP_ART_SCALE : 0));
  }
  assert(highest <= top_art_rest_y(crown) - TOP_HOP_PX);
  assert(top_art_figure(trophy, 0).pose.crown == Crown::None);
  // 6〜10: 跳んでいる間だけ左右反転を挟んで回り、着地では向きを戻す。
  assert(!top_art_figure(star, 0).flip && top_art_figure(star, TOP_SPIN_FLIP_MS).flip);
  assert(!top_art_figure(star, 2 * TOP_SPIN_FLIP_MS).flip && top_art_figure(star, 3 * TOP_SPIN_FLIP_MS).flip);
  for (uint16_t ms = TOP_HOP_AIR_MS; ms < TOP_HOP_MS; ms += TOP_ART_STEP_MS) assert(!top_art_figure(star, ms).flip);
  // 11〜25: 左右に揺れ、1周の後半にメダルが小→大→小ときらめく。
  assert(top_art_figure(medal, 500).x == TOP_ART_FIGURE_X + TOP_SWING_PX);
  assert(top_art_figure(medal, 1500).x == TOP_ART_FIGURE_X - TOP_SWING_PX);
  assert(top_glint_frame(TOP_GLINT_FROM_MS - TOP_ART_STEP_MS) == -1 && top_glint_frame(TOP_GLINT_FROM_MS) == 0);
  assert(top_glint_frame(TOP_GLINT_PEAK_MS) == 1 && top_glint_frame(TOP_GLINT_FADE_MS) == 0);
  assert(top_glint_frame(TOP_GLINT_TO_MS) == -1);
  // 26〜50: 喜びの顔でパタパタ羽ばたき（4コマを順に）、ゆっくり上下する。
  for (uint16_t ms = 0; ms < 4 * flap.flapMs; ms += flap.flapMs)
    assert(top_art_figure(flap, ms).pose.wing == ms / flap.flapMs);
  assert(top_art_figure(flap, 250).y < top_art_figure(flap, 750).y);
  // 下位50%: しょんぼりの顔で羽を上げた姿から、羽を下ろして体を沈める。羽ばたかず、向きも変えない。
  const TopArtFigure lifted = top_art_figure(TOP_ART_BOTTOM, 0);
  const TopArtFigure dropped = top_art_figure(TOP_ART_BOTTOM, TOP_DROOP_MS);
  assert(lifted.pose.face == MascotFace::Sad && lifted.pose.wing != MASCOT_WING_DOWN_FRAME);
  assert(dropped.pose.wing == MASCOT_WING_DOWN_FRAME && dropped.y == lifted.y + TOP_ART_SCALE);
  for (uint16_t ms = TOP_DROOP_MS; ms < TOP_ART_CYCLE_MS; ms += TOP_ART_STEP_MS) {
    const TopArtFigure f = top_art_figure(TOP_ART_BOTTOM, ms);
    assert(!f.flip && f.x == dropped.x && f.y == dropped.y && same_pose(f.pose, dropped.pose));
  }
  // 静かな表示では画素の変わらない段を同じとみなし、派手な動きの間は段ごとに変わるとみなす。
  const TopArtFrame still{true, false, TOP_DROOP_MS}, later{true, false, TOP_ART_CYCLE_MS - TOP_ART_STEP_MS};
  assert(same_top_art(&TOP_ART_BOTTOM, still, &TOP_ART_BOTTOM, later));
  assert(!same_top_art(&TOP_ART_BOTTOM, still, &TOP_ART_BOTTOM, {true, false, 0}));
  assert(!same_top_art(&medal, {true, true, 700}, &medal, {true, true, 750}));
  assert(!same_top_art(&medal, {true, false, 700}, &flap, {true, false, 700}));
  assert(!same_top_art(&medal, {true, false, 700}, &medal, {}) && same_top_art(&medal, {}, &flap, {}));
  // きらめきが変われば、マスコットが同じ位置でも描き直す。
  const uint16_t glintOn = TOP_GLINT_PEAK_MS;
  assert(top_glint_frame(glintOn) != top_glint_frame(glintOn - TOP_ART_STEP_MS));
}

static void test_mirror() {
  // 左右反転では、マスコットと持っている星を、マスコットの枠の中で左右対称の位置に描く。
  const TopArtTier& star = *top_art_tier(top(8));
  TopArtFigure flipped = top_art_figure(star, TOP_SPIN_FLIP_MS);
  assert(flipped.flip);
  TopArtFigure plain = flipped;
  plain.flip = false;
  Recorder a, b;
  draw_top_art_figure(a, star, plain, TOP_SPIN_FLIP_MS);
  draw_top_art_figure(b, star, flipped, TOP_SPIN_FLIP_MS);
  assert(a.rects.size() == b.rects.size());
  const int axis2 = 2 * plain.x + MASCOT_WIDTH * TOP_ART_SCALE; // 枠の中心の2倍
  for (const auto& r : a.rects) {
    const bool mirrored = std::any_of(b.rects.begin(), b.rects.end(), [&](const Recorder::Rect& m) {
      return m.y == r.y && m.h == r.h && m.w == r.w && m.color == r.color && m.x == axis2 - (r.x + r.w);
    });
    assert(mirrored);
  }
  // 反転していない星は右、反転した星は左に出る。
  uint16_t star_color = 0;
  assert(art_color(TOP_PROP_PALETTE, 'E', 0, star_color));
  auto star_left = [&](const Recorder& rec) {
    int left = 10000;
    for (const auto& r : rec.rects) if (r.color == star_color) left = std::min(left, r.x);
    return left;
  };
  assert(star_left(a) > plain.x + MASCOT_WIDTH * TOP_ART_SCALE / 2 && star_left(b) < plain.x);
}

int main() {
  test_tiers();
  test_timeline();
  test_pager();
  test_inside_area();
  test_motions();
  test_mirror();
  std::puts("PASS: top percent art tiers at 1/2, 5/6, 10/11, 25/26 and 50, none for null, a sad mascot for the bottom half, "
            "bursts only once on the first normal ② after the fill, quiet loop otherwise, stop on page change, "
            "time-driven frames, drawing inside the header area, mirrored props");
  return 0;
}
