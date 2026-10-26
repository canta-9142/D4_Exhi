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
  frame.fillRect(361, 81, 118, 36, TFT_BLACK);
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

void draw_info(const PlayerStatus& status, const DisplayChanges& changes) {
  frame.setFont(&fonts::Font0);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  if (changes.score) draw_info_number(status.score, status.known, 121);
  if (changes.remaining) draw_info_number(status.remaining, status.known, 174);
  if (changes.ready) {
    const char* label = !status.known ? "READY: --" : status.ready ? "READY" : "NOT READY";
    frame.fillRect(366, 151, 108, 12, TFT_BLACK);
    frame.setTextSize(1);
    frame.drawString(label, 366, 151);
  }
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
  frame.drawString("pt", 438, 121);
  frame.drawString("s", 438, 174);
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
  packetRenderer.draw(snapshot.cards, changes.cards);
  if (changes.self) draw_button(360, 0, snapshot.selfId, false);
  draw_neighbor_buttons(snapshot.selfId, snapshot.pressedMask, changes.buttons);
  if (changes.label) draw_game_status(snapshot);
  draw_info(snapshot.status, changes);
}

DisplayFrameStats draw_snapshot(const DisplaySnapshot& snapshot) {
  const uint32_t started = micros();
  const auto changes = display_changes(previousDisplay, snapshot, !displayDrawn);
  DisplayFrameStats stats;
  if (changes.any()) {
    const uint32_t scanStarted = micros();
    presenter.sample_scan();
    const uint32_t scanUs = micros() - scanStarted;
    const uint32_t drawStarted = micros();
    // backは2フレーム前の場合がある。表示済み状態とは別の基準で描き直す。
    const auto redraw = display_changes(previousBackDisplay, snapshot, !backDisplayDrawn);
    if (!backDisplayDrawn) draw_static_layout();
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
