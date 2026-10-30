#include <algorithm>
#include <cassert>
#include <cstdio>
#include <vector>
#include "../src/player/playing_effects.hpp"

// fillRectを記録する描画先。
struct Recorder {
  struct Rect { int x, y, w, h; uint16_t color; };
  std::vector<Rect> rects;
  void fillRect(int x, int y, int w, int h, uint16_t color) { rects.push_back({x, y, w, h, color}); }
};

// 16msごとに発行するゲーム側と同じ間隔で、comboとrankを与えて進める。
struct Timeline {
  PlayingEffects effects;
  uint32_t now = 5000;
  uint16_t combo = 0;
  uint8_t rank = 0;
  explicit Timeline(uint32_t start = 5000) : now(start) {}
  const EffectsFrame& set(uint16_t c, uint8_t r) {
    combo = c;
    rank = r;
    effects.update(combo, rank, true, now);
    return effects.frame();
  }
  const EffectsFrame& run(uint32_t ms) {
    for (uint32_t elapsed = 0; elapsed < ms; elapsed += 16) { // millis()の周回をまたいでも進める。
      now += 16;
      effects.update(combo, rank, true, now);
    }
    return effects.frame();
  }
};

static void test_tiers_and_milestones() {
  // 段階の境界（2・3、4・5、9・10、19・20）。
  const std::pair<uint16_t, MascotForm> forms[] = {
    {0, MascotForm::None}, {2, MascotForm::None}, {3, MascotForm::Slow}, {4, MascotForm::Slow},
    {5, MascotForm::Sway}, {9, MascotForm::Sway}, {10, MascotForm::Pair}, {19, MascotForm::Pair},
    {20, MascotForm::Blaze}, {999, MascotForm::Blaze},
  };
  for (const auto& entry : forms) assert(combo_tier(entry.first).form == entry.second);
  const std::pair<uint16_t, uint8_t> flames[] = {{0, 0}, {1, 10}, {2, 10}, {3, 20}, {4, 20}, {5, 30}, {9, 30},
                                                {10, 45}, {19, 45}, {20, 58}, {999, 58}};
  for (const auto& entry : flames) assert(combo_tier(entry.first).flameRows == entry.second);
  assert(!combo_tier(9).sparks && combo_tier(10).sparks && !combo_tier(19).hot && combo_tier(20).hot);

  // 区切り（5、10、20、30、以降10ごと）に達したときだけ。飛び越えたら達した最大の区切り。
  assert(milestone_crossed(4, 5) == 5 && milestone_crossed(9, 10) == 10 && milestone_crossed(29, 30) == 30);
  assert(milestone_crossed(19, 20) == 20 && milestone_crossed(989, 990) == 990 && milestone_crossed(998, 999) == 0);
  assert(milestone_crossed(3, 7) == 5 && milestone_crossed(5, 12) == 10 && milestone_crossed(8, 31) == 30);
  assert(milestone_crossed(5, 7) == 0 && milestone_crossed(10, 19) == 0 && milestone_crossed(0, 4) == 0);
  assert(milestone_crossed(5, 5) == 0 && milestone_crossed(10, 5) == 0 && milestone_crossed(30, 30) == 0);

  Timeline t;
  t.set(4, 0);
  t.run(200);
  assert(t.set(5, 0).milestoneMs == 0 && t.effects.frame().milestone == 5);
  assert(t.run(MILESTONE_MS - FLAME_STEP_MS).milestoneMs != EFFECT_OFF);
  assert(t.run(200).milestoneMs == EFFECT_OFF);
  t.set(5, 0); // 同じ値の再通知では出さない。
  assert(t.run(32).milestoneMs == EFFECT_OFF);
  assert(t.set(7, 0).milestoneMs == EFFECT_OFF);
  assert(t.set(9, 0).milestoneMs == EFFECT_OFF);
  assert(t.set(10, 0).milestoneMs == 0 && t.effects.frame().milestone == 10);
  t.run(1000);
  t.set(29, 0);
  assert(t.effects.frame().milestone == 20); // 10→29は20を飛び越えた。
  t.run(1000);
  assert(t.set(30, 0).milestoneMs == 0 && t.effects.frame().milestone == 30);
  t.run(1000);
  // 3→7のように区切りを飛び越えても出す。
  Timeline jump;
  jump.set(3, 0);
  jump.run(100);
  assert(jump.set(7, 0).milestoneMs == 0 && jump.effects.frame().milestone == 5);
  // 区切りの演出中は、流れているマスコットが喜びの顔で羽ばたき、左右反転を挟む。
  LeftMascot mascots[MASCOT_SLOTS];
  bool flipped = false, straight = false;
  for (int i = 0; i < 9; ++i) {
    const EffectsFrame& f = jump.run(FLAME_STEP_MS);
    if (f.milestoneMs == EFFECT_OFF) break;
    const uint8_t count = flying_mascots(f, 0, mascots);
    assert(count == 1 && mascots[0].pose.face == MascotFace::Joy);
    (mascots[0].flip ? flipped : straight) = true;
  }
  assert(flipped && straight);
  // 入ったときの値では出さない（画面を離れていた間の変化を演出しない）。
  Timeline enter;
  assert(enter.set(30, 2).milestoneMs == EFFECT_OFF && enter.effects.frame().rankUpMs == EFFECT_OFF);
}

static void test_rank_up() {
  // 冠は1〜3位で金・銀・銅、4位以下とnullでなし。
  assert(crown_for_rank(1) == Crown::Gold && crown_for_rank(2) == Crown::Silver && crown_for_rank(3) == Crown::Bronze);
  for (uint8_t rank : {0, 4, 5, 8}) assert(crown_for_rank(rank) == Crown::None);
  Timeline t;
  t.set(12, 0);
  t.run(1000);
  LeftMascot mascots[MASCOT_SLOTS];
  for (uint8_t rank = 0; rank <= 8; ++rank) {
    const uint8_t count = flying_mascots(t.effects.frame(), rank, mascots);
    assert(count >= 1);
    for (uint8_t i = 0; i < count; ++i) assert(mascots[i].pose.crown == crown_for_rank(rank));
  }

  // nullから順位が付いても、下がっても出さない。上がったときだけ約0.8秒。
  assert(t.set(12, 4).rankUpMs == EFFECT_OFF && !t.effects.rank_flash());
  assert(t.set(12, 6).rankUpMs == EFFECT_OFF);
  t.run(100);
  assert(t.set(12, 2).rankUpMs == 0 && t.effects.frame().rankUpRank == 2 && t.effects.rank_flash());
  // 情報欄の数字は同じ期間、緑に点滅させる。
  bool lit = false, dark = false;
  uint32_t lastActive = 0;
  const uint32_t started = t.now;
  for (int i = 0; i < 70; ++i) {
    t.run(16);
    if (t.effects.frame().rankUpMs != EFFECT_OFF) lastActive = t.now - started;
    if (t.now - started < RANK_UP_MS) (t.effects.rank_flash() ? lit : dark) = true;
    else assert(!t.effects.rank_flash());
  }
  assert(lit && dark && lastActive + FLAME_STEP_MS >= RANK_UP_MS && lastActive < RANK_UP_MS + FLAME_STEP_MS);
  assert(t.set(12, 3).rankUpMs == EFFECT_OFF);
  assert(t.set(12, 1).rankUpMs == 0 && t.effects.frame().rankUpRank == 1);
  // nullになったら止める。
  assert(t.set(12, 0).rankUpMs == EFFECT_OFF && !t.effects.rank_flash());
  assert(t.set(12, 2).rankUpMs == EFFECT_OFF);
  // 流せない画面では比べない。戻ったときに光らせない。
  t.effects.update(12, 5, false, t.now);
  assert(t.effects.frame() == EffectsFrame{} && !t.effects.running());
  t.effects.update(12, 3, false, t.now + 16);
  assert(t.set(12, 2).rankUpMs == EFFECT_OFF && !t.effects.rank_flash());
  // マスコットが飛んでいないときも演出は出す（文字・火花・矢印は描画側がrankUpMsで描く）。
  Timeline still;
  still.set(1, 5);
  still.run(100);
  assert(still.set(1, 3).rankUpMs == 0 && still.effects.frame().form == MascotForm::None);
  // 区切りと同時なら両方。
  Timeline both;
  both.set(9, 4);
  both.run(100);
  both.combo = 10;
  both.rank = 3;
  both.effects.update(10, 3, true, both.now);
  assert(both.effects.frame().rankUpMs == 0 && both.effects.frame().milestoneMs == 0);
  // millis()の周回をまたいでも、演出の時刻と順位の点滅は途切れない。
  Timeline wrap(UINT32_MAX - 300);
  wrap.set(12, 5);
  wrap.run(200);
  assert(wrap.set(12, 3).rankUpMs == 0 && wrap.effects.rank_flash());
  uint32_t lastFlight = wrap.effects.frame().flightMs;
  for (int i = 0; i < 40; ++i) {
    const EffectsFrame& f = wrap.run(16);
    assert(f.flightMs >= lastFlight && f.flightMs - lastFlight <= FLAME_STEP_MS);
    lastFlight = f.flightMs;
  }
  assert(wrap.now < 1000 && wrap.effects.frame().rankUpMs != EFFECT_OFF && wrap.effects.frame().flameLit);
  wrap.run(RANK_UP_MS);
  assert(wrap.effects.frame().rankUpMs == EFFECT_OFF && !wrap.effects.rank_flash());
  // 矢印と火花は左の列の中を動く。
  for (uint16_t ms = 0; ms < RANK_UP_MS; ms += FLAME_STEP_MS) {
    for (uint8_t i = 0; i < RANK_UP_ARROWS; ++i) {
      int x, y;
      rank_up_arrow(i, ms, x, y);
      assert(x >= LEFT_COLUMN_X && x + 10 <= LEFT_COLUMN_X + LEFT_COLUMN_WIDTH && y >= 0 && y + 10 <= 240);
    }
    for (uint8_t i = 0; i < CELEBRATE_SPARKS; ++i) {
      int x, y;
      celebrate_spark(i, ms, RANK_UP_MS, LEFT_CENTER_X, RANK_UP_SPARK_Y, RANK_UP_SPARK_RX, RANK_UP_SPARK_RY, x, y);
      assert(x >= LEFT_COLUMN_X && x + CELEBRATE_SPARK_SIZE <= LEFT_COLUMN_X + LEFT_COLUMN_WIDTH);
      celebrate_spark(i, ms, MILESTONE_MS, LEFT_CENTER_X, MILESTONE_SPARK_Y, MILESTONE_SPARK_R, MILESTONE_SPARK_R, x, y);
      assert(x >= LEFT_COLUMN_X && x + CELEBRATE_SPARK_SIZE <= LEFT_COLUMN_X + LEFT_COLUMN_WIDTH && y + 4 <= 240);
    }
  }
  // 文字は数字3（y=28〜51）と2（y=108〜131）の間、区切りは2と1（y=188〜211）の間に収まる。
  assert(RANK_UP_TEXT_Y - TEXT_BACK_PAD >= 52 && RANK_UP_NUMBER_Y + 24 + TEXT_BACK_PAD <= 108);
  assert(MILESTONE_TEXT_Y - TEXT_BACK_PAD >= 132 && MILESTONE_TEXT_Y + 16 + TEXT_BACK_PAD <= 188);
  assert(COMBO_TEXT_Y >= 212 && COMBO_TEXT_Y + 16 <= 239 && COMBO_TEXT_X + 4 * 12 <= 120); // 「x999」
  assert(left_text_size(10) == 2 && left_text_size(11) == 1); // 「COMBO x90!」は2、「COMBO x100!」は1。
  assert(left_text_size(8) == 2); // 「RANK UP!」
}

static void test_break() {
  Timeline t;
  t.set(12, 2);
  const EffectsFrame flying = t.run(1500);
  LeftMascot before[MASCOT_SLOTS];
  const uint8_t flyingCount = flying_mascots(flying, 2, before);
  assert(flyingCount >= 1);
  // 3以上から減ったらBREAK。流れていたマスコットは「× ×」の顔で羽ばたきを止め、冠は付けたまま少し落ちる。
  const EffectsFrame& broken = t.set(4, 2);
  assert(broken.breakTextMs == 0 && broken.breakMs == 0 && broken.brokenForm == MascotForm::Pair);
  assert(broken.form == MascotForm::None && broken.milestoneMs == EFFECT_OFF && broken.smokeMs == 0);
  LeftMascot falling[MASCOT_SLOTS];
  assert(broken_mascots(broken, 2, falling) == flyingCount);
  for (uint8_t i = 0; i < flyingCount; ++i) {
    assert(falling[i].pose.face == MascotFace::Sad && falling[i].pose.crown == Crown::Silver);
    assert(falling[i].pose.wing == 1 && !falling[i].pose.aura && !falling[i].flip);
    assert(falling[i].x == before[i].x);
  }
  int lastY = falling[0].y;
  bool smoked = false;
  for (int i = 0; i < 20; ++i) {
    const EffectsFrame& f = t.run(FLAME_STEP_MS);
    LeftMascot m[MASCOT_SLOTS];
    if (f.breakMs != EFFECT_OFF && f.breakMs < BREAK_FALL_MS) {
      assert(broken_mascots(f, 2, m) == flyingCount && m[0].y >= lastY && m[0].pose.wing == 1);
      lastY = m[0].y;
    } else {
      assert(broken_mascots(f, 2, m) == 0);
      int x, y;
      uint16_t ms;
      if (broken_smoke(f, 0, x, y, ms)) smoked = true; // 落ち終えたら煙のマスになって消える。
    }
    if (f.breakTextMs != EFFECT_OFF) assert(f.breakTextMs < BREAK_TEXT_MS);
  }
  assert(smoked && lastY > falling[0].y);
  const EffectsFrame& after = t.effects.frame();
  int x, y;
  uint16_t ms;
  assert(after.breakMs == EFFECT_OFF && !broken_smoke(after, 0, x, y, ms) && after.breakTextMs == EFFECT_OFF);
  // 減った後も3以上なら、BREAKが終わってから新しい段階の姿が下端から出る。
  assert(after.form == MascotForm::Slow && after.flightMs + BREAK_MS <= 20 * 80 + FLAME_STEP_MS);
  MascotFlight flight;
  assert(mascot_flight(MascotForm::Slow, 0, 0, flight) && flight.y >= 240);

  // 3未満からの減少ではBREAKを出さないが、炎の煙は上げる。
  Timeline low;
  low.set(2, 0);
  low.run(1000);
  const EffectsFrame& dropped = low.set(0, 0);
  assert(dropped.breakTextMs == EFFECT_OFF && dropped.breakMs == EFFECT_OFF && dropped.smokeMs == 0);
  // 5→1のように0を経由しなくても、減ったら切れたと解釈する。
  Timeline skip;
  skip.set(5, 0);
  skip.run(1000);
  assert(skip.set(1, 0).breakTextMs == 0);
}

static void test_flight() {
  // 0〜2では出さない。
  for (uint16_t combo : {0, 1, 2}) {
    Timeline t;
    t.set(combo, 1);
    for (int i = 0; i < 100; ++i) assert(t.run(50).form == MascotForm::None);
  }
  // 位置は時刻だけで決まる。発行の間隔が違っても、同じ時刻なら同じ位置になる。
  for (MascotForm form : {MascotForm::Slow, MascotForm::Sway, MascotForm::Pair, MascotForm::Blaze}) {
    for (uint32_t ms = 0; ms < 20000; ms += FLAME_STEP_MS) {
      for (uint8_t slot = 0; slot < MASCOT_SLOTS; ++slot) {
        MascotFlight a, b;
        const bool visible = mascot_flight(form, slot, ms, a);
        assert(visible == mascot_flight(form, slot, ms, b));
        if (!visible) continue;
        assert(a.x == b.x && a.y == b.y);
        // 縁取りを含めて左の列（x=1〜119）の外に出ない。上下は下端の外から上端の外まで。
        assert(a.x - MASCOT_DRAWN_MARGIN >= LEFT_COLUMN_X);
        assert(a.x + MASCOT_DRAWN_WIDTH + MASCOT_DRAWN_MARGIN <= LEFT_COLUMN_X + LEFT_COLUMN_WIDTH);
        assert(a.y <= FLIGHT_BOTTOM && a.y >= FLIGHT_TOP);
      }
    }
  }
  assert(FLIGHT_BOTTOM - MASCOT_DRAWN_MARGIN - MASCOT_SCALE >= 240); // 出始めは縁取りも見えない。
  assert(FLIGHT_TOP + MASCOT_DRAWN_HEIGHT + MASCOT_DRAWN_MARGIN <= 0); // 上端の外で消える。
  Timeline fast(100), slow(100);
  fast.set(12, 0);
  slow.set(12, 0);
  for (int i = 0; i < 200; ++i) {
    fast.run(16);
    if (i % 3 == 2) {
      slow.now = fast.now;
      slow.effects.update(12, 0, true, slow.now);
      assert(slow.effects.frame() == fast.effects.frame());
    }
  }
  // 下端から上端まで、3〜4は約3秒、5〜9は約2.5秒。上端まで昇ったら下から出直す。
  MascotFlight f;
  assert(mascot_flight(MascotForm::Slow, 0, 0, f) && f.y == FLIGHT_BOTTOM);
  assert(mascot_flight(MascotForm::Slow, 0, SLOW_PERIOD_MS - 1, f) && f.y < FLIGHT_TOP + 2);
  assert(mascot_flight(MascotForm::Slow, 0, SLOW_PERIOD_MS, f) && f.y == FLIGHT_BOTTOM);
  assert(mascot_flight(MascotForm::Sway, 0, SWAY_PERIOD_MS, f) && f.y == FLIGHT_BOTTOM);
  // 揺れる姿は左右に動き、2体の姿は2体目が遅れて下端から出る。
  int minX = 999, maxX = -999;
  for (uint32_t ms = 0; ms < SWAY_PERIOD_MS; ms += FLAME_STEP_MS) {
    assert(mascot_flight(MascotForm::Sway, 0, ms, f));
    minX = std::min<int>(minX, f.x);
    maxX = std::max<int>(maxX, f.x);
  }
  assert(maxX - minX >= 20);
  assert(!mascot_flight(MascotForm::Pair, 1, PAIR_PERIOD_MS / 2 - 1, f));
  assert(mascot_flight(MascotForm::Pair, 1, PAIR_PERIOD_MS / 2, f) && f.y == FLIGHT_BOTTOM);
  assert(!mascot_flight(MascotForm::Slow, 1, 0, f) && !mascot_flight(MascotForm::Pair, 2, 0, f));

  // 段階が上がると、その段階の姿を下端からすぐに出し直す。
  Timeline up;
  up.set(3, 0);
  assert(up.run(1500).flightMs >= 1400);
  const EffectsFrame& raised = up.set(5, 0);
  assert(raised.form == MascotForm::Sway && raised.flightMs == 0);
  LeftMascot mascots[MASCOT_SLOTS];
  assert(flying_mascots(raised, 0, mascots) == 1 && mascots[0].y >= 240 - MASCOT_SCALE);
  // 同じ段階の中で増えても出し直さない。
  up.run(500);
  const uint32_t ms = up.effects.frame().flightMs;
  assert(up.set(6, 0).flightMs == ms);

  // 5〜9は頬を点滅させる。
  bool blushOn = false, blushOff = false;
  for (int i = 0; i < 10; ++i) {
    up.run(FLAME_STEP_MS);
    flying_mascots(up.effects.frame(), 0, mascots);
    (mascots[0].pose.blush ? blushOn : blushOff) = true;
  }
  assert(blushOn && blushOff);

  // 20以上では、飛んでいるマスコットがすべて炎をまとい、高速の1体が約4秒に1回だけ出る。
  Timeline blaze;
  blaze.set(20, 1);
  int dashes = 0;
  bool dashing = true; // 段階に入った時刻に1体目が出る。
  uint32_t dashMs = 0;
  for (const EffectsFrame* fr = &blaze.effects.frame(); fr->flightMs < 12000; fr = &blaze.run(16)) {
    const uint8_t count = flying_mascots(*fr, 1, mascots);
    MascotFlight probe;
    const bool dash = mascot_flight(fr->form, 2, fr->flightMs, probe);
    for (uint8_t k = 0; k < count; ++k) assert(mascots[k].pose.aura && mascots[k].pose.crown == Crown::Gold);
    if (dash) {
      // 高速の1体は喜びの顔で、ほかの2体より速く昇る。
      assert(count >= 2 && mascots[count - 1].pose.face == MascotFace::Joy && probe.flapMs < 120);
      if (!dashing) ++dashes;
      dashMs += 16;
    }
    dashing = dash;
  }
  assert(dashes == 2 && mascot_flight(MascotForm::Blaze, 2, 0, f)); // 0秒・4秒・8秒の3回。
  assert(dashMs >= 3 * (DASH_MS - 100) && dashMs <= 3 * (DASH_MS + 100));
  Timeline pair;
  pair.set(10, 0);
  pair.run(3000);
  assert(flying_mascots(pair.effects.frame(), 0, mascots) == 2 && !mascots[0].pose.aura);
  // 2体の姿は通り道に火の粉を散らす。左の列の中に置く。
  int trails = 0;
  for (int i = 0; i < 40; ++i) {
    const EffectsFrame& fr = pair.run(FLAME_STEP_MS);
    for (uint8_t slot = 0; slot < 2; ++slot)
      for (uint8_t age = 1; age <= TRAIL_STEPS; ++age) {
        TrailSpark spark;
        if (!trail_spark(fr, PlayingEffects::SEED, slot, age, spark)) continue;
        ++trails;
        assert(spark.x >= LEFT_COLUMN_X && spark.x + TRAIL_SIZE <= LEFT_COLUMN_X + LEFT_COLUMN_WIDTH);
        assert(spark.level >= 1 && spark.level <= TRAIL_STEPS);
      }
  }
  assert(trails > 0);
  TrailSpark none;
  assert(!trail_spark(up.effects.frame(), PlayingEffects::SEED, 0, 1, none)); // 5〜9では散らさない。

  // 自動操作（comboが常に0）では出ない。
  Timeline autoMode;
  autoMode.set(0, 0);
  for (int i = 0; i < 100; ++i) {
    const EffectsFrame& fr = autoMode.run(100);
    assert(fr.form == MascotForm::None && fr.breakMs == EFFECT_OFF && !fr.flameLit && fr.milestoneMs == EFFECT_OFF);
  }
}

static void test_mascot_art() {
  // まばたきは2〜4秒おき。同じシードなら同じ並び。
  BlinkTimer a(42), b(42), c(43);
  uint32_t previous = 0;
  bool differs = false;
  std::vector<uint32_t> starts;
  bool blinking = false;
  for (uint32_t ms = 0; ms < 60000; ms += 10) {
    const bool on = a.update(ms);
    assert(on == b.update(ms));
    differs |= on != c.update(ms);
    if (on && !blinking) starts.push_back(ms);
    blinking = on;
  }
  assert(differs && starts.size() >= 14);
  for (uint32_t start : starts) {
    if (previous) assert(start - previous >= BlinkTimer::MIN_INTERVAL_MS - 10 && start - previous <= BlinkTimer::MAX_INTERVAL_MS + 10);
    previous = start;
  }
  assert(starts[0] >= BlinkTimer::MIN_INTERVAL_MS - 10 && starts[0] <= BlinkTimer::MAX_INTERVAL_MS + 10);
  // ゲーム中のマスコットもまばたきする。
  Timeline t;
  t.set(3, 0);
  bool sawBlink = false;
  LeftMascot mascots[MASCOT_SLOTS];
  for (int i = 0; i < 120; ++i) {
    const EffectsFrame& f = t.run(FLAME_STEP_MS);
    if (flying_mascots(f, 0, mascots) && mascots[0].pose.face == MascotFace::Blink) sawBlink = true;
  }
  assert(sawBlink);

  // 部品を重ねた結果は、冠を含めて24×17の枠に収まる。縁取りはその外側2画素まで。
  for (uint8_t wing = 0; wing < 4; ++wing)
    for (uint8_t face = 0; face < 4; ++face)
      for (uint8_t legs = 0; legs < 2; ++legs)
        for (uint8_t crown = 0; crown < 4; ++crown)
          for (bool aura : {false, true}) {
            MascotPose pose;
            pose.wing = wing;
            pose.face = MascotFace(face);
            pose.legs = legs;
            pose.crown = Crown(crown);
            pose.blush = face % 2;
            pose.aura = aura;
            pose.auraTick = uint8_t(wing * 7 + face);
            const MascotCanvas canvas(pose);
            bool crownTop = false;
            for (int y = 0; y < MascotCanvas::HEIGHT; ++y)
              for (int x = 0; x < MascotCanvas::WIDTH; ++x) {
                const char key = canvas.at(x, y);
                if (key == ART_CLEAR) continue;
                const bool inFrame = x >= MASCOT_AURA_MARGIN && x < MASCOT_AURA_MARGIN + MASCOT_WIDTH &&
                                     y >= MASCOT_AURA_MARGIN && y < MASCOT_AURA_MARGIN + MASCOT_HEIGHT;
                if (key == 'A' || key == 'a') assert(aura); // 縁取りは炎をまとう姿だけ。枠の中の隙間にも置く。
                else assert(inFrame);
                uint16_t color;
                assert(art_color(MASCOT_PALETTE, key, 0, color)); // パレットにない文字を使わない。
                if (y == MASCOT_AURA_MARGIN && (key == 'Y' || key == 'L' || key == 'C')) crownTop = true;
              }
            assert(crownTop == (pose.crown != Crown::None));
            // 金・銀・銅は形が同じで色だけ違う。
            if (pose.crown == Crown::Silver) assert(canvas.at(MASCOT_AURA_MARGIN + 6, MASCOT_AURA_MARGIN) == 'L');
            if (pose.crown == Crown::Bronze) assert(canvas.at(MASCOT_AURA_MARGIN + 6, MASCOT_AURA_MARGIN + 4) == 'c');
            // 体の青はノードの色に変えない。顔の文字は黒。
            assert(canvas.at(MASCOT_AURA_MARGIN + 9, MASCOT_AURA_MARGIN + 7) == 'B');
            assert(canvas.at(MASCOT_AURA_MARGIN + 7, MASCOT_AURA_MARGIN + 6) == ART_BLACK || face != 0); // 縦長の目は黒。
          }
  // 2倍で描いた矩形は枠（縁取りを含む）の中に収まる。
  MascotPose pose;
  pose.crown = Crown::Gold;
  pose.aura = true;
  Recorder plain, flipped;
  draw_mascot(plain, pose, 2, 30, 50, false);
  draw_mascot(flipped, pose, 2, 30, 50, true);
  assert(!plain.rects.empty() && plain.rects.size() == flipped.rects.size());
  for (const auto& r : plain.rects) {
    assert(r.x >= 30 - MASCOT_DRAWN_MARGIN && r.x + r.w <= 30 + MASCOT_DRAWN_WIDTH + MASCOT_DRAWN_MARGIN);
    assert(r.y >= 50 - MASCOT_DRAWN_MARGIN && r.y + r.h <= 50 + MASCOT_DRAWN_HEIGHT + MASCOT_DRAWN_MARGIN);
    assert(r.w % 2 == 0 && r.h == 2);
  }
  // 左右反転は枠の中心を軸に左右対称の位置に描く。
  const int left = 30 - MASCOT_DRAWN_MARGIN, right = 30 + MASCOT_DRAWN_WIDTH + MASCOT_DRAWN_MARGIN;
  for (const auto& r : plain.rects) {
    const bool mirrored = std::any_of(flipped.rects.begin(), flipped.rects.end(), [&](const Recorder::Rect& m) {
      return m.y == r.y && m.w == r.w && m.color == r.color && m.x == left + right - (r.x + r.w);
    });
    assert(mirrored);
  }
  // 共通の文字: 透明・白・黒・自ノードの色。
  uint16_t color;
  assert(!art_color(MASCOT_PALETTE, ART_CLEAR, 0x1234, color));
  assert(art_color(MASCOT_PALETTE, ART_WHITE, 0, color) && color == 0xffff);
  assert(art_color(ARROW_PALETTE, ART_BLACK, 0, color) && color == 0);
  assert(art_color(ARROW_PALETTE, ART_SELF, 0x1234, color) && color == 0x1234);
  // 輪郭は濃い紺、足は明るい灰色、体はマスコットの青（下書きの色）。
  assert(art_color(MASCOT_PALETTE, 'K', 0, color) && color == art_rgb(0x26307a) && color != 0);
  assert(art_color(MASCOT_PALETTE, 'N', 0, color) && color == art_rgb(0xc9cad6));
  assert(art_color(MASCOT_PALETTE, 'B', 0, color) && color == art_rgb(0x3f4bd0));
  Recorder arrow;
  draw_art(arrow, UP_ARROW_PART.frame(0), ARROW_PALETTE, 2, 10, 20, false, 0);
  for (const auto& r : arrow.rects) assert(r.x >= 10 && r.x + r.w <= 20 && r.y >= 20 && r.y + r.h <= 30);
}

int main() {
  test_tiers_and_milestones();
  test_rank_up();
  test_break();
  test_flight();
  test_mascot_art();
  std::puts("PASS: combo tiers and milestones, rank crowns and RANK UP only on improvement, BREAK with the sad fall "
            "and smoke, time-driven flight inside the left column, blaze dashes every 4 s, mascot parts, blinks and mirroring");
}
