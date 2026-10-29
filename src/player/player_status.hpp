#ifndef PLAYER_STATUS_HPP_
#define PLAYER_STATUS_HPP_

#include <cstdint>

enum class ControlMode : uint8_t { Auto, Manual };

struct PlayerStatus {
  int32_t score = 0;
  uint8_t remaining = 0;
  bool ready = false;
  bool known = false;
  // ゲーム時間の起点までの秒数。サーバーの値をそのまま表示し、MCUでは減らさない。
  uint8_t countdown = 0;
};

// 結果の正本はsummary。statusや現在の操作モードから補完しない。
struct FinalSummary {
  int32_t score = 0;
  uint8_t rank = 0;
  bool eligible = false;
  bool known = false;
  uint32_t revision = 0;
  bool animate = false;
  // 上位何%か（1〜50）。0はnullで、上位%のページを出さない。
  uint8_t topPercent = 0;
};

class FinishedAnimation {
public:
  uint16_t update(const FinalSummary& summary, bool enabled, uint32_t nowMs) {
    if (!enabled || !summary.animate) {
      running_ = false;
      revision_ = summary.revision;
      return 1000;
    }
    if (revision_ != summary.revision) {
      revision_ = summary.revision;
      startedMs_ = nowMs;
      running_ = true;
    }
    if (!running_) return 1000;
    const uint32_t elapsed = nowMs - startedMs_;
    if (elapsed >= 1200) { running_ = false; return 1000; }
    return static_cast<uint16_t>(elapsed * 1000 / 1200);
  }
private:
  uint32_t revision_ = 0;
  uint32_t startedMs_ = 0;
  bool running_ = false;
};

// カウントダウンが表示中に0へ変わったとき、GO!を出す期間を返す。表示だけの演出で通信状態は変えない。
class CountdownAnimation {
public:
  static constexpr uint32_t GO_MS = 500;

  bool update(uint8_t countdown, bool enabled, uint32_t nowMs) {
    if (!enabled) {
      counting_ = running_ = false;
      return false;
    }
    if (countdown) {
      counting_ = true;
      running_ = false;
      return false;
    }
    if (counting_) {
      counting_ = false;
      running_ = true;
      startedMs_ = nowMs;
    }
    if (running_ && nowMs - startedMs_ >= GO_MS) running_ = false;
    return running_;
  }
private:
  uint32_t startedMs_ = 0;
  bool counting_ = false;
  bool running_ = false;
};

#endif // PLAYER_STATUS_HPP_
