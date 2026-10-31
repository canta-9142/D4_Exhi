#ifndef PREPARING_RENDERER_HPP_
#define PREPARING_RENDERER_HPP_

#include "frame_buffer.hpp"
#include "display_snapshot.hpp"

// 案内の文字を横方向の中央に描く。停止中・モード選択・準備完了の待機・結果など、パケットが流れない画面で使う。
inline void preparing_text(FrameBuffer& frame, const char* text, int y, int size = 2) {
  frame.setFont(&fonts::Font0);
  frame.setTextSize(size);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString(text, (FrameBuffer::WIDTH - frame.textWidth(text)) / 2, y);
}

inline const char* preparing_wait_label(const DisplaySnapshot& s) {
  if (s.selfId == NodeId::None) return "CONNECTING";
  if (!s.synced) return "SYNCING";
  return "WAIT STATUS";
}

inline void preparing_start_wait(FrameBuffer& frame, int y) {
  preparing_text(frame, "Waiting for the", y);
  preparing_text(frame, "administrator to start.", y + 24);
}

// 下向きの矢印（実際のボタンを指す）。先端はtop+lengthの行。
inline void draw_down_arrow(FrameBuffer& frame, int x, int top, int length, uint16_t color) {
  frame.fillRect(x - 2, top, 4, length - 16, color);
  frame.fillTriangle(x - 12, top + length - 16, x + 12, top + length - 16, x, top + length, color);
}

// 見出しの隅に、段階の残り秒数を出す。10秒以下は注意色。出さないときは空にする。
inline void draw_header_window(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(HEADER_WINDOW_X, HEADER_WINDOW_Y, HEADER_WINDOW_WIDTH, HEADER_WINDOW_HEIGHT, TFT_BLACK);
  const int seconds = header_window(s);
  if (seconds < 0) return;
  char text[8];
  snprintf(text, sizeof(text), "%ds", seconds);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(3);
  frame.setTextColor(seconds <= WINDOW_WARNING_SECONDS ? TFT_ORANGE : TFT_WHITE, TFT_BLACK);
  frame.drawString(text, HEADER_WINDOW_X, HEADER_WINDOW_Y);
}

// 停止中（休止中の表示）・モード選択・準備完了の待機（一斉ゲーム方式）の見出し。
inline const char* lobby_title(const DisplaySnapshot& s) {
  switch (display_screen(s)) {
    case DisplayScreen::Standby: return "STANDBY";
    case DisplayScreen::Select: return "SELECT";
    default: return "CLOSED";
  }
}

inline void draw_lobby_layout(FrameBuffer& frame, const char* title) {
  frame.fillScreen(TFT_BLACK);
  frame.setFont(&fonts::Font0);
  frame.setTextSize(3);
  frame.setTextColor(TFT_WHITE, TFT_BLACK);
  frame.drawString(title, 24, 20);
  frame.drawFastHLine(0, 80, FrameBuffer::WIDTH, TFT_WHITE);
}

inline const char* round_label(RoundKind round) {
  return round == RoundKind::Tutorial ? "TUTORIAL" : round == RoundKind::Normal ? "NORMAL" : "--";
}

// 練習（TUTORIAL）は緑。モード選択の枠、練習ラウンドの札とPRACTICEの表示に使う。
constexpr uint16_t TUTORIAL_COLOR = TFT_GREEN, SELECT_NORMAL_COLOR = TFT_WHITE, SELECT_DIM_COLOR = 0x39E7;

// モード選択の枠（左から2番目が練習、3番目が通常）。ラベルと、実際のボタンを指す矢印を描く。
// 選んだ枠は色で塗って強調し、もう一方は暗くする。選んでいなければ両方を通常の色で描く。
inline void draw_select_frame(FrameBuffer& frame, int index, const char* label, uint16_t color,
                              bool chosen, bool dimmed) {
  const int x = button_frame_x(index);
  const uint16_t line = dimmed ? SELECT_DIM_COLOR : color;
  frame.fillRect(x, BUTTON_FRAME_Y, BUTTON_FRAME_WIDTH, BUTTON_FRAME_HEIGHT, chosen ? color : TFT_BLACK);
  frame.drawRect(x + 1, BUTTON_FRAME_Y + 1, BUTTON_FRAME_WIDTH - 2, BUTTON_FRAME_HEIGHT - 2, line);
  frame.drawRect(x + 2, BUTTON_FRAME_Y + 2, BUTTON_FRAME_WIDTH - 4, BUTTON_FRAME_HEIGHT - 4, line);
  const int center = x + BUTTON_FRAME_WIDTH / 2;
  frame.setFont(&fonts::Font0);
  frame.setTextSize(2);
  frame.setTextColor(chosen ? TFT_BLACK : line, chosen ? color : TFT_BLACK);
  frame.drawString(label, center - frame.textWidth(label) / 2, 252);
  draw_down_arrow(frame, center, 280, 32, chosen ? TFT_BLACK : line);
}

inline void draw_select_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  const LobbyState state = lobby_state(s);
  if (state == LobbyState::Auto) {
    preparing_text(frame, "AUTO", 120, 4);
    preparing_text(frame, "Please wait.", 184);
    return;
  }
  const bool confirming = state == LobbyState::Confirming;
  if (confirming) {
    preparing_text(frame, "CONFIRMING...", 112, 3);
    preparing_text(frame, "Waiting for confirmation.", 168);
  } else {
    preparing_text(frame, "TUTORIAL: free practice", 120);
    preparing_text(frame, "NORMAL: 90-second round", 160);
  }
  const bool tutorial = s.chooseRound == RoundKind::Tutorial, normal = s.chooseRound == RoundKind::Normal;
  draw_select_frame(frame, 1, "TUTORIAL", TUTORIAL_COLOR, confirming && tutorial, confirming && normal);
  draw_select_frame(frame, 2, "NORMAL", SELECT_NORMAL_COLOR, confirming && normal, confirming && tutorial);
}

inline void draw_lobby_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(0, 81, 480, 239, TFT_BLACK);
  if (lobby_state(s) == LobbyState::Waiting) {
    preparing_text(frame, preparing_wait_label(s), 128, 3);
    preparing_text(frame, "Please wait.", 184);
  } else if (display_screen(s) == DisplayScreen::Select) {
    draw_select_body(frame, s);
  } else if (display_screen(s) == DisplayScreen::Standby) {
    // 一斉ゲーム方式で、モード選択を終えた準備完了。選んだ種別を出して、管理者の開始を待つ。
    preparing_text(frame, round_label(s.status.round), 96, 3);
    preparing_text(frame, "READY", 136, 4);
    preparing_start_wait(frame, 192);
  } else {
    preparing_text(frame, "OUT OF SERVICE", 112, 3);
    preparing_text(frame, "The exhibit is not running.", 168);
  }
}

inline void render_lobby(FrameBuffer& frame, const DisplaySnapshot& s, const DisplayChanges& changes) {
  if (changes.lobbyHeader) {
    frame.fillRect(24, 48, 320, 24, TFT_BLACK);
    frame.setFont(&fonts::Font0);
    frame.setTextSize(1);
    frame.setTextColor(TFT_WHITE, TFT_BLACK);
    frame.drawString(lobby_state(s) == LobbyState::Waiting ? preparing_wait_label(s) :
                     s.mode == ControlMode::Manual ? "MANUAL" : "AUTO", 24, 56);
  }
  if (changes.headerWindow) draw_header_window(frame, s);
  if (changes.lobbyBody) draw_lobby_body(frame, s);
}

#endif // PREPARING_RENDERER_HPP_
