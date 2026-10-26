#ifndef PACKET_RENDERER_HPP_
#define PACKET_RENDERER_HPP_

#include <algorithm>
#include <array>
#include "frame_buffer.hpp"
#include "frame_damage.hpp"
#include "display_snapshot.hpp"
#include "node_config.hpp"

class PacketRenderer {
public:
  explicit PacketRenderer(FrameBuffer& frame) : frame_(frame) {}

  void draw(const PacketAnimation::Snapshot& cards, const PacketDirtyRows& rows) {
    for (int row = 0; row < ANIMATION_HEIGHT;) {
      if (!rows[row]) { ++row; continue; }
      const int top = row;
      while (row < ANIMATION_HEIGHT && rows[row]) ++row;
      frame_.setClipRect(ANIMATION_X, ANIMATION_Y + top, ANIMATION_WIDTH, row - top);
      frame_.fillRect(ANIMATION_X, ANIMATION_Y + top, ANIMATION_WIDTH, row - top, TFT_BLACK);
      frame_.setFont(&fonts::Font0);
      frame_.setTextDatum(lgfx::textdatum_t::top_left);
      frame_.setTextPadding(0);
      for (const auto& card : cards) {
        if (card.active) draw_card(card, top, row);
      }
      frame_.clearClipRect();
    }
  }

private:
  void draw_card(const PacketCard& card, int clipTop, int clipBottom) {
    const int y = static_cast<int>(card.y);
    const int top = std::max(clipTop, y);
    const int bottom = std::min(clipBottom, y + PACKET_CARD_HEIGHT);
    const uint8_t opacity = card.opacity();
    if (!opacity || top >= bottom) return;
    if (opacity < 255)
      frame_.readRect(ANIMATION_X, ANIMATION_Y + top, ANIMATION_WIDTH, bottom - top, background_.data());
    const auto& visual = nodeVisuals[static_cast<int>(card.packet.destination)];
    const int absoluteY = ANIMATION_Y + y;
    frame_.fillRect(ANIMATION_X, absoluteY, ANIMATION_WIDTH, PACKET_CARD_HEIGHT, TFT_LIGHTGRAY);
    frame_.fillRect(ANIMATION_X, absoluteY, 40, PACKET_CARD_HEIGHT, visual.color);
    frame_.drawRect(ANIMATION_X, absoluteY, ANIMATION_WIDTH, PACKET_CARD_HEIGHT, TFT_WHITE);
    frame_.setTextColor(TFT_BLACK, TFT_LIGHTGRAY);
    frame_.setTextSize(4);
    frame_.drawChar(visual.name, ANIMATION_X + 80, absoluteY + 14);
    frame_.setTextSize(2);
    char label[7];
    snprintf(label, sizeof(label), "#%3d", card.packet.id);
    frame_.drawString(label, ANIMATION_X + ANIMATION_WIDTH - frame_.textWidth(label) - 4,
                      absoluteY + PACKET_CARD_HEIGHT - 18);
    if (opacity < 255) {
      for (int line = 0; line < bottom - top; ++line) {
        for (int x = 0; x < ANIMATION_WIDTH; ++x) {
          blendRow_[x] = background_[line * ANIMATION_WIDTH + x];
          blendRow_[x].a = 255 - opacity;
        }
        // PSRAM上で画素ごとに合成せず、内部RAMの一行上で同じLGFXの合成処理を使う。
        frame_.readRect(ANIMATION_X, ANIMATION_Y + top + line, ANIMATION_WIDTH, 1, foregroundRow_.data());
        lgfx::pixelcopy_t blend;
        blend.src_data = blendRow_.data();
        blend.src_bitwidth = ANIMATION_WIDTH;
        lgfx::pixelcopy_t::blend_rgb_fast<lgfx::swap565_t, lgfx::argb8888_t>(
            foregroundRow_.data(), 0, ANIMATION_WIDTH, &blend);
        frame_.pushImage(ANIMATION_X, ANIMATION_Y + top + line, ANIMATION_WIDTH, 1, foregroundRow_.data());
      }
    }
  }

  FrameBuffer& frame_;
  std::array<lgfx::swap565_t, ANIMATION_WIDTH * PACKET_CARD_HEIGHT> background_{};
  std::array<lgfx::swap565_t, ANIMATION_WIDTH> foregroundRow_{};
  std::array<lgfx::argb8888_t, ANIMATION_WIDTH> blendRow_{};
};

#endif // PACKET_RENDERER_HPP_
