#ifndef FRAME_PRESENTER_HPP_
#define FRAME_PRESENTER_HPP_

#include <algorithm>
#include <array>
#include "display.hpp"
#include "frame_buffer.hpp"
#include "frame_damage.hpp"
#include "soft_te.hpp"

struct DisplayFrameStats {
  uint32_t scanReadUs = 0, drawUs = 0, packUs = 0, scanWaitUs = 0;
  uint32_t transferUs = 0, frameUs = 0, bytes = 0, spans = 0;
};

class FramePresenter {
public:
  FramePresenter(Display& lcd, FrameBuffer& frame) : lcd_(lcd), frame_(frame), timing_(lcd) {}
  FramePresenter(const FramePresenter&) = delete;
  FramePresenter& operator=(const FramePresenter&) = delete;
  ~FramePresenter() {
    lcd_.waitDMA();
    for (auto* buffer : dma_) heap_caps_free(buffer);
  }

  bool begin() {
    for (auto& buffer : dma_) {
      buffer = static_cast<lgfx::swap565_t*>(heap_caps_malloc(
          FrameDamage::TILE * FrameBuffer::HEIGHT * sizeof(lgfx::swap565_t), MALLOC_CAP_INTERNAL | MALLOC_CAP_DMA));
      if (!buffer) return false;
    }
    return true;
  }

  void calibrate() { timing_.calibrate(); }
  void sample_scan() { timing_.sample(); }

  DisplayFrameStats present(const FrameDamage& damage) {
    DisplayFrameStats stats;
    if (!damage.changedTiles()) return stats;
    std::array<ScanTransferBlock, FrameDamage::COLUMNS> blocks{};
    std::array<unsigned, FrameDamage::COLUMNS> pixels{}, spans{};
    damage.forEachSpan([&](int x, int, int w, int h) {
      pixels[x / FrameDamage::TILE] += w * h;
      ++spans[x / FrameDamage::TILE];
    });
    const float pixelDmaUs = 16000000.0f / lcd_.getPanel()->getBus()->getClock();
    float earliest = 0, latest = 0;
    unsigned count = 0;
    for (unsigned i = 0; i < pixels.size(); ++i) {
      if (!pixels[i]) continue;
      latest += pixels[i] * (pixelDmaUs + packPixelUs_) + spans[i] * 20;
      blocks[count++] = {int(i * FrameDamage::TILE), FrameDamage::TILE, earliest, latest};
      earliest += pixels[i] * pixelDmaUs;
    }
    // ここまでに差分を確定し、全てのDMAが完了してから次のswapへ進む。
    frame_.swap();
    uint32_t transferStarted = 0;
    damage.forEachSpan([&](int x, int y, int w, int h) {
      auto* buffer = dma_[stats.spans % dma_.size()];
      const uint32_t packStarted = micros();
      frame_.copyFrontColumns(x, y, w, h, buffer);
      const uint32_t packUs = micros() - packStarted;
      stats.packUs += packUs;
      packPixelUs_ = std::max(packPixelUs_, float(packUs) / (w * h) * 1.25f);
      if (!stats.spans) {
        const uint32_t waitStarted = micros();
        timing_.wait_for_start(blocks, count);
        stats.scanWaitUs = micros() - waitStarted;
        transferStarted = micros();
        lcd_.beginColumnWrite();
      }
      lcd_.waitDMA();
      lcd_.pushColumnBlockDMA(x, y, w, h, buffer);
      stats.bytes += w * h * sizeof(lgfx::swap565_t);
      ++stats.spans;
    });
    lcd_.endColumnWrite();
    stats.transferUs = micros() - transferStarted;
    return stats;
  }

private:
  Display& lcd_;
  FrameBuffer& frame_;
  DisplayTransferTiming timing_;
  std::array<lgfx::swap565_t*, 2> dma_{};
  float packPixelUs_ = 0.12f;
};

#endif // FRAME_PRESENTER_HPP_
