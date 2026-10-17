#ifndef PACKET_RENDERER_HPP_
#define PACKET_RENDERER_HPP_

#include <algorithm>
#include <array>
#include <bitset>
#include "display.hpp"
#include "node_config.hpp"
#include "packet_animation.hpp"
#include "soft_te.hpp"

class PacketRenderer {
public:
  PacketRenderer(Display& display, lgfx::LGFX_Sprite& sprite)
      : display_(display), sprite_(sprite), timing_(display) {}

  void begin() { timing_.calibrate(); }

  void draw(const PacketAnimation::Snapshot& snapshot) {
    if (!sprite_.getBuffer()) return;

    const auto dirtyRows = find_packet_dirty_rows(previous_, snapshot, drawn_);
    previous_ = snapshot;
    if (dirtyRows.none()) return;

    const auto damage = describe_packet_damage(dirtyRows);
    const auto estimate = measure_packet_transfer(damage);

    timing_.sample_scan();
    compose_packet_frame(snapshot);
    prepare_packet_frame(damage, estimate.mergeRows);
    transfer_packet_frame(damage, estimate);
    drawn_ = true;
  }

private:
  void draw_packet_card(int y, NodeId destination, int packet_id, uint8_t opacity = 255) {
    constexpr int h = PACKET_CARD_HEIGHT;
    const int top = std::max(0, y);
    const int bottom = std::min(ANIMATION_HEIGHT, y + h);

    if (!opacity || top >= bottom) return;
    if (opacity < 255) {
      sprite_.readRect(0, top, ANIMATION_WIDTH, bottom - top, background_.data());
    }

    const auto& visual = nodeVisuals[static_cast<int>(destination)];

    sprite_.fillRect(0, y, ANIMATION_WIDTH, h, TFT_LIGHTGRAY);
    sprite_.fillRect(0, y, 40, h, visual.color);
    sprite_.drawRect(0, y, ANIMATION_WIDTH, h, TFT_WHITE);
    sprite_.setTextColor(TFT_BLACK, TFT_LIGHTGRAY);
    sprite_.setTextSize(4);
    sprite_.drawChar(visual.name, 80, y + 14);
    sprite_.setTextSize(2);

    char label[7];
    snprintf(label, sizeof(label), "#%3d", packet_id);
    sprite_.drawString(label, ANIMATION_WIDTH - sprite_.textWidth(label) - 4, y + h - 18);

    if (opacity < 255) {
      for (int line = 0; line < bottom - top; ++line) {
        for (int x = 0; x < ANIMATION_WIDTH; ++x) {
          blendRow_[x] = background_[line * ANIMATION_WIDTH + x];
          blendRow_[x].a = 255 - opacity;
        }
        sprite_.pushAlphaImage(0, top + line, ANIMATION_WIDTH, 1, blendRow_.data());
      }
    }
  }

  using PacketDirtyRows = std::bitset<ANIMATION_HEIGHT>;

  void mark_card_rows(PacketDirtyRows& rows, const PacketCard& card) {
    if (!card.active) return;

    const int y = static_cast<int>(card.y);
    const int top = std::max(0, y);
    const int bottom = std::min(ANIMATION_HEIGHT, y + PACKET_CARD_HEIGHT);

    for (int row = top; row < bottom; ++row) {
      rows.set(row);
    }
  }

  bool same_card_pixels(const PacketCard& previous, const PacketCard& next) {
    if (previous.active != next.active) return false;
    if (!next.active) return true;

    return static_cast<int>(previous.y) == static_cast<int>(next.y) &&
           previous.packet.id == next.packet.id &&
           previous.packet.destination == next.packet.destination &&
           previous.opacity() == next.opacity();
  }

  PacketDirtyRows find_packet_dirty_rows(const PacketAnimation::Snapshot& previous,
                                       const PacketAnimation::Snapshot& snapshot, bool drawn) {
    PacketDirtyRows rows;
    if (!drawn) {
      rows.set();
      return rows;
    }
    
    for (size_t i = 0; i < snapshot.size(); ++i) {
      const auto& old = previous[i];
      const auto& next = snapshot[i];
      if (same_card_pixels(old, next)) continue;
      mark_card_rows(rows, old);
      mark_card_rows(rows, next);
    }
    return rows;
  }

  struct PacketDamage {
    PacketDirtyRows rows;
    int top = 0;
    int bottom = ANIMATION_HEIGHT;
    unsigned spanCount = 0;
  };

  PacketDamage describe_packet_damage(const PacketDirtyRows& rows) {
    PacketDamage damage{rows};

    while (!rows[damage.top]) ++damage.top;
    while (!rows[damage.bottom - 1]) --damage.bottom;

    for (int y = 0; y < ANIMATION_HEIGHT; ++y) {
      if (rows[y] && (y == 0 || !rows[y - 1])) ++damage.spanCount;
    }
    return damage;
  }

  void prepare_packet_band(int x, int width, int top, int height, size_t offset) {
    for (int y = 0; y < height; ++y) {
      sprite_.readRect(x, top + y, width, 1, rowPixels_.data());
      for (int col = 0; col < width; ++col) {
        columnPixels_[offset + col * height + y] = rowPixels_[col];
      }
    }
  }

  void send_packet_band(int x, int width, int top, int height, size_t offset) {
    display_.pushColumnBlockDMA(ANIMATION_X + x, ANIMATION_Y + top, width, height,
                                columnPixels_.data() + offset);
    display_.waitDMA();
  }

  template <typename F>
  void for_packet_bands(const PacketDamage& damage, bool mergeRows, F process) {
    if (mergeRows) {
      process(damage.top, damage.bottom - damage.top);
      return;
    }
    for (int row = 0; row < ANIMATION_HEIGHT;) {
      if (!damage.rows[row]) { ++row; continue; }
      const int top = row;
      while (row < ANIMATION_HEIGHT && damage.rows[row]) ++row;
      process(top, row - top);
    }
  }

  void prepare_packet_frame(const PacketDamage& damage, bool mergeRows) {
    size_t offset = 0;
    for_packet_bands(damage, mergeRows, [&](int top, int height) {
      prepare_packet_band(0, ANIMATION_WIDTH, top, height, offset);
      offset += ANIMATION_WIDTH * height;
    });
  }

  uint32_t measure_packet_block(int x, int width, const PacketDamage& damage, bool mergeRows) {
    uint32_t durationUs = 0;
    for_packet_bands(damage, mergeRows, [&](int top, int height) {
      prepare_packet_band(x, width, top, height, 0);
      const uint32_t startedUs = micros();
      send_packet_band(x, width, top, height, 0);
      durationUs += std::max<uint32_t>(1, micros() - startedUs);
    });
    return durationUs;
  }

  struct PacketTransferEstimate {
    bool mergeRows;
    float columnTimeUs;
  };

  PacketTransferEstimate measure_packet_transfer(const PacketDamage& damage) {
    timing_.wait_after_scan_read();
    display_.beginColumnWrite();

    uint32_t splitUs = 1, mergedUs = 1;
    for (int probe = 0; probe < 4; ++probe) {
      const bool mergeRows = damage.spanCount > 1 && probe >= 2;
      const uint32_t duration = measure_packet_block(probe * TRANSFER_PROBE_WIDTH,
          TRANSFER_PROBE_WIDTH, damage, mergeRows);
      
      if (probe == 1) splitUs = duration;
      if (probe == 3) mergedUs = duration;
    }
    display_.endColumnWrite();
    
    const bool mergeRows = damage.spanCount > 1 && mergedUs < splitUs;
    return {mergeRows, float(mergeRows ? mergedUs : splitUs) / TRANSFER_PROBE_WIDTH};
  }

  void compose_packet_frame(const PacketAnimation::Snapshot& snapshot) {
    sprite_.fillSprite(TFT_BLACK);
    
    for (const auto& card : snapshot) {
      if (!card.active) continue;
      draw_packet_card(static_cast<int>(card.y), card.packet.destination, card.packet.id, card.opacity());
    }
  }

  void transfer_packet_frame(const PacketDamage& damage, const PacketTransferEstimate& estimate) {
    const unsigned rowCount = estimate.mergeRows ? damage.bottom - damage.top : damage.rows.count();
    const unsigned spanCount = estimate.mergeRows ? 1 : damage.spanCount;
    
    timing_.wait_for_transfer_start(estimate.columnTimeUs, rowCount, spanCount);
    display_.beginColumnWrite();
    
    size_t offset = 0;
    for_packet_bands(damage, estimate.mergeRows, [&](int top, int height) {
      send_packet_band(0, ANIMATION_WIDTH, top, height, offset);
      offset += ANIMATION_WIDTH * height;
    });
    
    timing_.finish_transfer();
    display_.endColumnWrite();
    display_.clearClipRect();
  }

  Display& display_;
  lgfx::LGFX_Sprite& sprite_;
  PacketTransferTiming timing_;
  PacketAnimation::Snapshot previous_{};
  bool drawn_ = false;
  static constexpr int TRANSFER_PROBE_WIDTH = 16;
  std::array<lgfx::rgb565_t, ANIMATION_WIDTH * PACKET_CARD_HEIGHT> background_{};
  std::array<lgfx::argb8888_t, ANIMATION_WIDTH> blendRow_{};
  alignas(4) std::array<lgfx::swap565_t, ANIMATION_WIDTH> rowPixels_{};
  alignas(4) std::array<lgfx::swap565_t, ANIMATION_WIDTH * ANIMATION_HEIGHT> columnPixels_{};
};

#endif // PACKET_RENDERER_HPP_
