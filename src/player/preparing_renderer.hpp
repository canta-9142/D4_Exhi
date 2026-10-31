#ifndef PREPARING_RENDERER_HPP_
#define PREPARING_RENDERER_HPP_

#include "frame_buffer.hpp"
#include "display_snapshot.hpp"

// 案内の文字を横方向の中央に描く。停止中・準備完了の待機・結果など、パケットが流れない画面で使う。
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

// 停止中（休止中の表示）と、準備完了の待機（一斉ゲーム方式）の見出し。
inline const char* lobby_title(const DisplaySnapshot& s) {
  return display_screen(s) == DisplayScreen::Standby ? "STANDBY" : "CLOSED";
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

inline void draw_lobby_body(FrameBuffer& frame, const DisplaySnapshot& s) {
  frame.fillRect(0, 81, 480, 239, TFT_BLACK);
  if (lobby_state(s) == LobbyState::Waiting) {
    preparing_text(frame, preparing_wait_label(s), 128, 3);
    preparing_text(frame, "Please wait.", 184);
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
  if (changes.lobbyBody) draw_lobby_body(frame, s);
}

#endif // PREPARING_RENDERER_HPP_
