#ifndef DRAWING_HPP_
#define DRAWING_HPP_

#include <Arduino.h>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>
#include <freertos/task.h>
#include "node_config.hpp"
#include "display_snapshot.hpp"
#include "frame_presenter.hpp"
#include "packet_renderer.hpp"
#include "preparing_renderer.hpp"
#include "finished_renderer.hpp"
#include "countdown_renderer.hpp"

struct DrawingTaskContext {
  QueueHandle_t snapshots;
  TaskHandle_t readyTask;
};

FrameBuffer frame;
FramePresenter presenter{lcd, frame};
PacketRenderer packetRenderer{frame};
DisplaySnapshot previousDisplay{};
DisplaySnapshot previousBackDisplay{};
bool displayDrawn = false;
bool backDisplayDrawn = false;

void startup_animation() {
  constexpr uint32_t durationMs = 1000;
  const uint32_t startedMs = millis();
  bool first = true;
  for (;;) {
    const uint32_t progress = std::min<uint32_t>(millis() - startedMs, durationMs);
    char bar[] = "[..........]";
    for (int i = 0; i < 10; ++i) bar[i + 1] = i < int(progress * 10 / durationMs) ? '#' : '.';
    frame.fillRect(140, 112, 200, 96, TFT_BLACK);
    frame.setFont(&fonts::Font0);
    frame.setTextSize(2);
    frame.setTextColor(TFT_WHITE, TFT_BLACK);
    frame.drawString("STARTING", (480 - frame.textWidth("STARTING")) / 2, 120);
    frame.drawString(bar, (480 - frame.textWidth(bar)) / 2, 152);
    frame.setCursor((480 - frame.textWidth("100%")) / 2, 184);
    frame.printf("%3lu%%", (unsigned long)(progress * 100 / durationMs));
    FrameDamage damage;
    if (first) damage.full();
    else damage.markRect(140, 112, 200, 96);
    presenter.present(damage);
    first = false;
    if (progress == durationMs) break;
    delay(1);
  }
  delay(500);
}

void draw_button(int x, int y, NodeId id, bool pressed) {
  const auto& visual = nodeVisuals[static_cast<int>(id)];
  const uint16_t background = pressed ? TFT_BLACK : visual.color;
  const uint16_t foreground = id == NodeId::None ? TFT_WHITE : pressed ? visual.color : TFT_BLACK;
  frame.fillRect(x + 1, y + 1, 119, 79, background);
  frame.setFont(&fonts::Font0);
  frame.setTextColor(foreground, background);
  frame.setTextSize(6);
  frame.drawChar(visual.name, x + 42, y + 16);
}

void draw_neighbor_buttons(NodeId id, uint8_t pressedMask, uint8_t changedMask) {
  const auto& layout = nodeLayouts[static_cast<int>(id)];
  for (int i = 0; i < 4; ++i) {
    if (!(changedMask & (1u << i))) continue;
    const NodeId neighbor = layout.neighbors[i];
    if (neighbor == NodeId::None) {
      frame.fillRect(i * 120 + 1, 241, 119, 79, TFT_BLACK);
    } else draw_button(i * 120, 240, neighbor, pressedMask & (1u << i));
  }
}

const char* phase_label(GamePhase phase) {
  switch (phase) {
    case GamePhase::Preparing: return "PREPARING";
    case GamePhase::Playing: return "PLAYING";
    case GamePhase::Finished: return "FINISHED";
    case GamePhase::Unknown: return "WAITING";
  }
  return "WAITING";
}

const char* connection_label(const DisplaySnapshot& snapshot) {
  if (snapshot.selfId == NodeId::None) return "CONNECTING";
  if (!snapshot.synced) return "SYNCING";
  if (snapshot.phase == GamePhase::Unknown) return "WAIT PHASE";
  if (snapshot.awaitingStatus) return "WAIT STATUS";
  return snapshot.mode == ControlMode::Manual ? "MANUAL" : "AUTO";
}

void draw_game_status(const DisplaySnapshot& snapshot) {
  frame.fillRect(INFO_X, 81, INFO_WIDTH, INFO_HEADER_HEIGHT, TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.setTextSize(2);
  frame.drawString(phase_label(snapshot.phase), 366, 84);
  frame.setTextSize(1);
  frame.drawString(connection_label(snapshot), 366, 105);
}

void draw_info_number(int32_t value, bool known, int y) {
  char number[12] = "----";
  if (known) snprintf(number, sizeof(number), "%ld", static_cast<long>(value));
  frame.fillRect(366, y, 72, 24, TFT_BLACK);
  int size = 3;
  frame.setTextSize(size);
  while (size > 1 && frame.textWidth(number) > 72) frame.setTextSize(--size);
  frame.drawString(number, 438 - frame.textWidth(number), y + (24 - 8 * size) / 2);
}

// 暫定順位。対象外・null・未取得は「--」。順位が上がった後はRANK UPの間、緑に点滅させる。
void draw_info_rank(const DisplaySnapshot& snapshot) {
  const bool flash = displayed_rank_flash(snapshot);
  char text[4] = "--";
  if (snapshot.status.known && snapshot.status.rank) snprintf(text, sizeof(text), "#%u", unsigned(snapshot.status.rank));
  frame.fillRect(INFO_RANK_X, INFO_RANK_Y, INFO_RANK_WIDTH, INFO_RANK_HEIGHT, TFT_BLACK);
  frame.setTextSize(4);
  frame.setTextColor(flash ? RANK_UP_GREEN : TFT_WHITE, TFT_BLACK);
  frame.drawString(text, INFO_RANK_X + INFO_RANK_WIDTH - 4 - frame.textWidth(text), INFO_RANK_Y + 2);
}

// 獲得点を帯の中だけに描く。発信者の点は自ノードの色、中継の点は白。
void draw_info_gains(const DisplaySnapshot& snapshot) {
  frame.fillRect(INFO_X, GAIN_LANE_Y, INFO_WIDTH, GAIN_LANE_HEIGHT, TFT_BLACK);
  const auto& gains = displayed_gains(snapshot);
  frame.setClipRect(INFO_X, GAIN_LANE_Y, INFO_WIDTH, GAIN_LANE_HEIGHT);
  frame.setTextSize(2);
  for (size_t i = 0; i < gains.size(); ++i) {
    const GainFloat& gain = gains[i];
    if (!gain.active) continue;
    char text[4];
    snprintf(text, sizeof(text), "+%u", unsigned(gain.value));
    const uint16_t color = gain.value == RELAY_GAIN ? TFT_WHITE : nodeVisuals[static_cast<int>(snapshot.selfId)].color;
    frame.setTextColor(dim565(color, gain_level(gain))); // 背景は透過し、帯の黒の上に重ねる。
    frame.drawString(text, GAIN_COLUMN_X + int(i) * GAIN_COLUMN_PITCH, GAIN_START_Y - gain_offset(gain));
  }
  frame.clearClipRect();
}

void draw_info(const DisplaySnapshot& snapshot, const DisplayChanges& changes) {
  const PlayerStatus& status = snapshot.status;
  frame.setFont(&fonts::Font0);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  if (changes.score) draw_info_number(status.score, status.known, INFO_SCORE_Y);
  if (changes.remaining) draw_info_number(status.remaining, status.known, INFO_REMAINING_Y);
  if (changes.ready) {
    const char* label = !status.known ? "READY: --" : status.ready ? "READY" : "NOT READY";
    frame.fillRect(366, INFO_READY_Y, 108, 8, TFT_BLACK);
    frame.setTextSize(1);
    frame.drawString(label, 366, INFO_READY_Y);
  }
  if (changes.rank) draw_info_rank(snapshot);
  if (changes.gains) draw_info_gains(snapshot);
}

// 炎の熱の格子は描画側だけが持ち、スナップショットの進行の番号まで進める。どちらの面へ描くかに関係なく、1回だけ進める。
FlameField<FLAME_LEFT_COLUMNS> flameLeft{FLAME_LEFT_SEED};
FlameField<FLAME_RIGHT_COLUMNS> flameRight{FLAME_RIGHT_SEED};
uint8_t flameEpoch = 0;

void sync_flames(const EffectsFrame& effects) {
  if (!effects.epoch) return; // 演出を出さない画面。
  if (effects.epoch != flameEpoch) {
    flameEpoch = effects.epoch;
    flameLeft.clear(effects.flameStep);
    flameRight.clear(effects.flameStep);
  }
  flameLeft.advance_to(effects.flameStep, effects.flameFuel);
  flameRight.advance_to(effects.flameStep, effects.flameFuel);
}

// 同じ段階が横に続くマスは1回のfillRectにまとめる。
template <int Columns>
void draw_flame_cells(const FlameField<Columns>& field, int left, const EffectsFrame& effects) {
  for_each_flame_run(field, effects.flameScale, left, [&](int x, int y, int w, int h, uint8_t level) {
    frame.fillRect(x, y, w, h, flame_color(level, effects.flameHot));
  });
}

// 炎の火の粉と、コンボが切れたときの煙。
void draw_flame_extras(const EffectsFrame& effects, uint32_t seed, int columns, int left) {
  if (effects.sparks) {
    const int top = FLAME_ROWS - 1 - effects.flameRows - 6;
    const uint32_t first = effects.sparkAge > FLAME_SPARK_LIFE ? effects.sparkAge - FLAME_SPARK_LIFE : 0;
    for (uint32_t index = first; index <= effects.sparkAge; ++index) {
      FlameSpark spark;
      if (flame_spark(seed, columns, index, effects.sparkAge, top, spark))
        frame.fillRect(left + spark.column * FLAME_CELL, spark.row * FLAME_CELL, FLAME_CELL, FLAME_CELL,
                       art_rgb(FLAME_SPARK_RGB));
    }
  }
  if (effects.smokeMs != EFFECT_OFF) {
    const uint8_t count = flame_smoke_puffs(columns);
    const uint8_t level = smoke_level(effects.smokeMs);
    for (uint8_t i = 0; i < count; ++i) {
      int x, y;
      flame_smoke_cell(seed, columns, i, effects.smokeRows, effects.smokeMs, x, y);
      frame.fillRect(left + x, y, FLAME_CELL, FLAME_CELL, smoke_color(i, level));
    }
  }
}

// 黒い下地を付けて、左の列の中央に文字を描く。
void draw_left_label(const char* text, int y, int size, uint16_t color) {
  frame.setTextSize(size);
  const int width = frame.textWidth(text) - size; // 右端の1列は空き。
  const int x = LEFT_CENTER_X - width / 2;
  frame.fillRect(x - TEXT_BACK_PAD, y - TEXT_BACK_PAD, width + 2 * TEXT_BACK_PAD, 8 * size + 2 * TEXT_BACK_PAD, TFT_BLACK);
  frame.setTextColor(color);
  frame.drawString(text, x, y);
}

void draw_celebrate_sparks(uint16_t ms, uint16_t durationMs, int cy, int rx, int ry, uint16_t color) {
  for (uint8_t i = 0; i < CELEBRATE_SPARKS; ++i) {
    int x, y;
    celebrate_spark(i, ms, durationMs, LEFT_CENTER_X, cy, rx, ry, x, y);
    frame.fillRect(x, y, CELEBRATE_SPARK_SIZE, CELEBRATE_SPARK_SIZE, i % 2 ? color : TFT_WHITE);
  }
}

// 左の列（x=1〜119）を下から順に重ねて描き直す: 炎 → 火の粉・煙 → マスコット → 火花・矢印 → キュー位置の数字 → 文字。
void draw_left_column(const DisplaySnapshot& snapshot) {
  const EffectsFrame& effects = displayed_effects(snapshot);
  const uint8_t rank = snapshot.status.rank;
  frame.setClipRect(LEFT_COLUMN_X, LEFT_COLUMN_Y, LEFT_COLUMN_WIDTH, LEFT_COLUMN_HEIGHT);
  frame.fillRect(LEFT_COLUMN_X, LEFT_COLUMN_Y, LEFT_COLUMN_WIDTH, LEFT_COLUMN_HEIGHT, TFT_BLACK);
  if (effects.flameLit) draw_flame_cells(flameLeft, FLAME_LEFT_X, effects);
  draw_flame_extras(effects, FLAME_LEFT_SEED, FLAME_LEFT_COLUMNS, FLAME_LEFT_X);
  for (uint8_t slot = 0; slot < 2; ++slot) {
    for (uint8_t age = 1; age <= TRAIL_STEPS; ++age) {
      TrailSpark spark;
      if (trail_spark(effects, PlayingEffects::SEED, slot, age, spark))
        frame.fillRect(spark.x, spark.y, TRAIL_SIZE, TRAIL_SIZE, dim565(TRAIL_COLOR, spark.level * GAIN_LEVELS / TRAIL_STEPS));
    }
  }
  LeftMascot mascots[MASCOT_SLOTS];
  for (uint8_t i = 0, count = broken_mascots(effects, rank, mascots); i < count; ++i)
    draw_mascot(frame, mascots[i].pose, MASCOT_SCALE, mascots[i].x, mascots[i].y, mascots[i].flip);
  for (uint8_t slot = 0; slot < MASCOT_SLOTS; ++slot) {
    int x, y;
    uint16_t ms;
    if (!broken_smoke(effects, slot, x, y, ms)) continue;
    const uint8_t level = smoke_level(ms);
    for (uint8_t i = 0; i < MASCOT_SMOKE_PUFFS; ++i) {
      const SmokePuff puff = smoke_puff(PlayingEffects::SEED + slot, i, MASCOT_SMOKE_PUFFS, ms, MASCOT_SMOKE_SPREAD, MASCOT_SMOKE_RISE);
      frame.fillRect(x + puff.dx - MASCOT_SMOKE_SIZE / 2, y + puff.dy - MASCOT_SMOKE_SIZE / 2,
                     MASCOT_SMOKE_SIZE, MASCOT_SMOKE_SIZE, smoke_color(i, level));
    }
  }
  for (uint8_t i = 0, count = flying_mascots(effects, rank, mascots); i < count; ++i)
    draw_mascot(frame, mascots[i].pose, MASCOT_SCALE, mascots[i].x, mascots[i].y, mascots[i].flip);
  if (effects.rankUpMs != EFFECT_OFF) {
    draw_celebrate_sparks(effects.rankUpMs, RANK_UP_MS, RANK_UP_SPARK_Y, RANK_UP_SPARK_RX, RANK_UP_SPARK_RY, RANK_UP_GREEN);
    for (uint8_t i = 0; i < RANK_UP_ARROWS; ++i) {
      int x, y;
      rank_up_arrow(i, effects.rankUpMs, x, y);
      draw_art(frame, UP_ARROW_PART.frame(0), ARROW_PALETTE, 2, x, y, false, 0);
    }
  }
  if (effects.milestoneMs != EFFECT_OFF)
    draw_celebrate_sparks(effects.milestoneMs, MILESTONE_MS, MILESTONE_SPARK_Y, MILESTONE_SPARK_R, MILESTONE_SPARK_R, MILESTONE_YELLOW);
  // キュー位置の数字は炎とマスコットの上に、背景を透過して描き直す。
  frame.setFont(&fonts::Font0);
  frame.setTextSize(3);
  frame.setTextColor(TFT_WHITE);
  frame.drawChar('3', 51, 28);
  frame.drawChar('2', 51, 108);
  frame.drawChar('1', 51, 188);
  char text[12];
  if (const uint16_t combo = displayed_combo(snapshot)) {
    snprintf(text, sizeof(text), "x%u", unsigned(combo));
    frame.setTextSize(2);
    frame.setTextColor(COMBO_YELLOW);
    frame.drawString(text, COMBO_TEXT_X, COMBO_TEXT_Y);
  }
  if (effects.rankUpMs != EFFECT_OFF) {
    draw_left_label("RANK UP!", RANK_UP_TEXT_Y, 2, RANK_UP_GREEN);
    snprintf(text, sizeof(text), "#%u", unsigned(effects.rankUpRank));
    draw_left_label(text, RANK_UP_NUMBER_Y, 3, TFT_WHITE);
  }
  if (effects.breakTextMs != EFFECT_OFF) draw_left_label("BREAK", MILESTONE_TEXT_Y, 2, BREAK_GRAY);
  if (effects.milestoneMs != EFFECT_OFF) {
    snprintf(text, sizeof(text), "COMBO x%u!", unsigned(effects.milestone));
    draw_left_label(text, MILESTONE_TEXT_Y, left_text_size(strlen(text)), COMBO_YELLOW);
  }
  frame.clearClipRect();
}

// カード欄の右の余白（x=320〜359）。炎と火の粉・煙だけを描き、境界線（x=360）には触れない。
void draw_right_margin(const DisplaySnapshot& snapshot) {
  const EffectsFrame& effects = displayed_effects(snapshot);
  frame.setClipRect(RIGHT_MARGIN_X, RIGHT_MARGIN_Y, RIGHT_MARGIN_WIDTH, RIGHT_MARGIN_HEIGHT);
  frame.fillRect(RIGHT_MARGIN_X, RIGHT_MARGIN_Y, RIGHT_MARGIN_WIDTH, RIGHT_MARGIN_HEIGHT, TFT_BLACK);
  if (effects.flameLit) draw_flame_cells(flameRight, FLAME_RIGHT_X, effects);
  draw_flame_extras(effects, FLAME_RIGHT_SEED, FLAME_RIGHT_COLUMNS, FLAME_RIGHT_X);
  frame.clearClipRect();
}

void draw_static_layout() {
  frame.fillScreen(TFT_BLACK);
  frame.drawLine(360, 0, 360, 240, TFT_WHITE);
  frame.drawLine(360, 80, 479, 80, TFT_WHITE);
  frame.drawLine(0, 240, 479, 240, TFT_WHITE);
  for (int x : {120, 240, 360}) frame.drawLine(x, 240, x, 319, TFT_WHITE);
  frame.setFont(&fonts::Font0);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.setTextSize(3);
  frame.drawChar('3', 51, 28);
  frame.drawChar('2', 51, 108);
  frame.drawChar('1', 51, 188);
  frame.drawString("pt", 438, INFO_SCORE_Y);
  frame.drawString("s", 438, INFO_REMAINING_Y);
  frame.setTextSize(1);
  frame.drawString("RANK", 366, INFO_RANK_Y + 13);
}

void draw_screen_layout(const DisplaySnapshot& snapshot) {
  if (preparing_screen(snapshot)) draw_preparing_layout(frame);
  else if (display_screen(snapshot) == DisplayScreen::Finished) draw_result_layout(frame, false);
  else if (display_screen(snapshot) == DisplayScreen::Preview) draw_result_layout(frame, true);
  else if (display_screen(snapshot) == DisplayScreen::Countdown) frame.fillScreen(TFT_BLACK);
  else draw_static_layout();
}

bool initialize_display(bool startup = true) {
  if (!lcd_init()) return false;
  if (!frame.begin() || !presenter.begin()) {
    lcd.setTextSize(2);
    lcd.drawString("Framebuffer allocation failed", 8, 244);
    return false;
  }
  if (startup) startup_animation();
  presenter.calibrate();
  draw_static_layout();
  return true;
}

void render_snapshot(const DisplaySnapshot& snapshot, const DisplayChanges& changes) {
  frame.setTextDatum(lgfx::textdatum_t::top_left);
  frame.setTextPadding(0);
  if (display_screen(snapshot) == DisplayScreen::Finished || display_screen(snapshot) == DisplayScreen::Preview) {
    if (changes.self) draw_button(360, 0, snapshot.selfId, false);
    if (display_screen(snapshot) == DisplayScreen::Preview) {
      if (changes.previewBody) render_preview(frame, snapshot);
    } else render_finished(frame, snapshot, changes);
    return;
  }
  if (display_screen(snapshot) == DisplayScreen::Countdown) {
    if (changes.self) draw_button(360, 0, snapshot.selfId, false);
    if (changes.countdownBody) render_countdown(frame, snapshot);
    return;
  }
  if (preparing_screen(snapshot)) {
    if (changes.self) draw_button(360, 0, snapshot.selfId, false);
    render_preparing(frame, snapshot, changes);
    return;
  }
  packetRenderer.draw(displayed_cards(snapshot), changes.cards);
  if (changes.self) draw_button(360, 0, snapshot.selfId, false);
  draw_neighbor_buttons(snapshot.selfId, snapshot.pressedMask, changes.buttons);
  if (changes.label) draw_game_status(snapshot);
  draw_info(snapshot, changes);
  if (changes.leftColumn) draw_left_column(snapshot);
  if (changes.rightMargin) draw_right_margin(snapshot);
}

DisplayFrameStats draw_snapshot(const DisplaySnapshot& snapshot) {
  const uint32_t started = micros();
  sync_flames(displayed_effects(snapshot));
  const auto changes = display_changes(previousDisplay, snapshot, !displayDrawn);
  DisplayFrameStats stats;
  if (changes.any()) {
    const uint32_t scanStarted = micros();
    presenter.sample_scan();
    const uint32_t scanUs = micros() - scanStarted;
    const uint32_t drawStarted = micros();
    // backは2フレーム前の場合がある。表示済み状態とは別の基準で描き直す。
    const auto redraw = display_changes(previousBackDisplay, snapshot, !backDisplayDrawn);
    if (!backDisplayDrawn || redraw.full) draw_screen_layout(snapshot);
    render_snapshot(snapshot, redraw);
    const auto damage = display_damage(changes, !displayDrawn);
    const uint32_t drawUs = micros() - drawStarted;
    stats = presenter.present(damage);
    stats.scanReadUs = scanUs;
    stats.drawUs = drawUs;
    previousBackDisplay = previousDisplay;
    backDisplayDrawn = displayDrawn;
  }
  previousDisplay = snapshot;
  displayDrawn = true;
  stats.frameUs = micros() - started;
  return stats;
}

void display_task(void* argument) {
  const auto& context = *static_cast<const DrawingTaskContext*>(argument);
  if (!initialize_display()) {
    vTaskDelete(nullptr);
    return;
  }
  bool readyNotified = false;
  for (;;) {
    DisplaySnapshot snapshot;
    if (xQueueReceive(context.snapshots, &snapshot, portMAX_DELAY) != pdTRUE) continue;
    draw_snapshot(snapshot);
    if (!readyNotified) {
      xTaskNotifyGive(context.readyTask);
      readyNotified = true;
    }
    vTaskDelay(1);
  }
}

#endif // DRAWING_HPP_
