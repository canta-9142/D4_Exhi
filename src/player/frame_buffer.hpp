#ifndef FRAME_BUFFER_HPP_
#define FRAME_BUFFER_HPP_

#include <array>
#include <cassert>
#include <cstring>
#include <utility>
#include <esp_heap_caps.h>
#include <LovyanGFX.hpp>
#include <lgfx/v1/panel/Panel_FrameBufferBase.hpp>

// CPU描画用の行優先RGB565。LCDへの転送は所有者が別途行う。
class FrameBuffer : public lgfx::LGFX_Device {
public:
  static constexpr int WIDTH = 480;
  static constexpr int HEIGHT = 320;
  static constexpr size_t PIXELS = WIDTH * HEIGHT;
  static constexpr size_t BYTES = PIXELS * sizeof(lgfx::swap565_t);

  FrameBuffer() {
    auto cfg = panel_.config();
    cfg.panel_width = cfg.memory_width = WIDTH;
    cfg.panel_height = cfg.memory_height = HEIGHT;
    panel_.config(cfg);
    setPanel(&panel_);
  }

  FrameBuffer(const FrameBuffer&) = delete;
  FrameBuffer& operator=(const FrameBuffer&) = delete;

  ~FrameBuffer() {
    heap_caps_free(front_);
    heap_caps_free(back_);
  }

  bool begin() {
    if (front_ || back_) return false;
    front_ = allocate();
    back_ = allocate();
    if (!front_ || !back_) {
      heap_caps_free(front_);
      heap_caps_free(back_);
      front_ = back_ = nullptr;
      return false;
    }
    panel_.bind(back_);
    return init_without_reset(true);
  }

  const lgfx::swap565_t* front() const { return front_; }
  lgfx::swap565_t* back() { return back_; }

  // 呼出側で旧frontの読み出し・DMA完了を確認してから交換する。
  void swap() {
    std::swap(front_, back_);
    panel_.bind(back_);
  }

  void copyFrontColumns(int x, int y, int w, int h, lgfx::swap565_t* destination) const {
    assert(x >= 0 && y >= 0 && w > 0 && h > 0 && x + w <= WIDTH && y + h <= HEIGHT);
    // PSRAMは行内で連続して読み、内部RAM側で列優先へ並べ替える。
    for (int row = 0; row < h; ++row) {
      const auto* source = front_ + (y + row) * WIDTH + x;
      for (int col = 0; col < w; ++col) destination[col * h + row] = source[col];
    }
  }

private:
  class Panel : public lgfx::Panel_FrameBufferBase {
  public:
    void bind(lgfx::swap565_t* pixels) {
      for (int y = 0; y < HEIGHT; ++y) lines_[y] = reinterpret_cast<uint8_t*>(pixels + y * WIDTH);
      _lines_buffer = lines_.data();
    }

    // このメモリをDMAへ直接渡さないため、cache write-backは不要。
    void display(uint_fast16_t, uint_fast16_t, uint_fast16_t, uint_fast16_t) override {}

  private:
    std::array<uint8_t*, HEIGHT> lines_{};
  } panel_;

  static lgfx::swap565_t* allocate() {
    return static_cast<lgfx::swap565_t*>(heap_caps_calloc(PIXELS, sizeof(lgfx::swap565_t),
                                                       MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
  }

  lgfx::swap565_t* front_ = nullptr;
  lgfx::swap565_t* back_ = nullptr;
};

#endif // FRAME_BUFFER_HPP_
