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
  // ベストスコア3種。NO_BESTはnullで、画面では「---」を出す。
  int32_t gameBest = NO_BEST;
  uint8_t gameBestNodes = 0; // bit n-1 がノードn（1〜8）。
  int32_t hourBest = NO_BEST;
  int8_t hourBestStart = -1; // 日本時間の0〜23。-1はnull。
  bool hourBestMine = false;
  int32_t allTimeBest = NO_BEST;
  bool allTimeBestMine = false;

  static constexpr int32_t NO_BEST = -1;
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

enum class ResultPage : uint8_t { Score, TopPercent };

// 結果画面のページ。筐体だけの表示状態で、サーバーへは何も送らない。
class ResultPager {
public:
  static constexpr uint8_t NEXT_BUTTON = 1u << 3;
  static constexpr uint32_t INTRO_MS = 1000;

  // finished: 確認表示でないFinished。ready: 確定結果を表示できる。next: 右端ボタンを押した。
  void update(const FinalSummary& summary, bool finished, bool ready, bool next, uint32_t nowMs) {
    // Finishedから離れたとき・設定の無効化・別世代の結果では1ページ目へ戻す。再同期中は保つ。
    if (!finished || !summary.known || !summary.topPercent || summary.revision != revision_) {
      page_ = ResultPage::Score;
      introShown_ = running_ = false;
      revision_ = summary.revision;
    }
    if (next && ready && summary.known && summary.topPercent) {
      page_ = page_ == ResultPage::Score ? ResultPage::TopPercent : ResultPage::Score;
      // 終了演出と同じく、通常の終了遷移で初めて見るときだけ演出する。
      if (page_ == ResultPage::TopPercent && summary.animate && !introShown_) {
        introShown_ = running_ = true;
        startedMs_ = nowMs;
      }
    }
    if (running_ && (page_ != ResultPage::TopPercent || !ready || !summary.animate ||
                     nowMs - startedMs_ >= INTRO_MS)) running_ = false;
    progress_ = running_ ? static_cast<uint16_t>((nowMs - startedMs_) * 1000 / INTRO_MS) : 1000;
  }
  ResultPage page() const { return page_; }
  uint16_t progress() const { return progress_; }
private:
  uint32_t revision_ = 0;
  uint32_t startedMs_ = 0;
  uint16_t progress_ = 1000;
  ResultPage page_ = ResultPage::Score;
  bool introShown_ = false;
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
