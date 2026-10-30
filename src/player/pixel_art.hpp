#ifndef PIXEL_ART_HPP_
#define PIXEL_ART_HPP_

#include <cstdint>
#include "effect_random.hpp"

// ドット絵の素材。1画素を1文字で書き、ソースのまま編集・差し替えできるようにする。
// 全素材で共通の文字: '.' 透明、'W' 白、'X' 黒、'S' 自ノードの色。ほかの文字は素材ごとのパレットで決める。
constexpr char ART_CLEAR = '.', ART_WHITE = 'W', ART_BLACK = 'X', ART_SELF = 'S';

constexpr uint16_t art_rgb(uint32_t rgb) {
  return static_cast<uint16_t>(((rgb >> 8) & 0xf800) | ((rgb >> 5) & 0x07e0) | ((rgb >> 3) & 0x001f));
}

struct ArtColor {
  char key;
  uint16_t color; // RGB565
};

struct ArtPalette {
  const ArtColor* colors;
  uint8_t count;
};

// 1コマ。rows[y][x]で1画素の文字を返す。
struct ArtFrame {
  const char* const* rows;
  uint8_t width, height;
};

// コマの並びとコマの長さ。frameMsが0なら時刻では進めず、呼び出し側がコマを選ぶ。
struct ArtPart {
  const char* const* const* frames;
  uint8_t count, width, height;
  uint16_t frameMs;
  ArtFrame frame(uint8_t index) const { return {frames[index % count], width, height}; }
};

// 共通の文字をパレットより先に解決する。見つからない文字は透明として扱う。
inline bool art_color(const ArtPalette& palette, char key, uint16_t selfColor, uint16_t& color) {
  switch (key) {
    case ART_CLEAR: return false;
    case ART_WHITE: color = 0xffff; return true;
    case ART_BLACK: color = 0x0000; return true;
    case ART_SELF: color = selfColor; return true;
  }
  for (uint8_t i = 0; i < palette.count; ++i) {
    if (palette.colors[i].key == key) {
      color = palette.colors[i].color;
      return true;
    }
  }
  return false;
}

// 1コマを倍率scaleで(x, y)から描く。flipなら左右反転する。描画先はfillRect(x, y, w, h, color)を持つもの。
// 同じ色が横に続く画素は1回のfillRectにまとめる。はみ出しの切り取りは描画先のクリップに任せる。
template <typename Target>
void draw_art(Target& target, const ArtFrame& art, const ArtPalette& palette, int scale, int x, int y,
              bool flip, uint16_t selfColor) {
  for (int row = 0; row < art.height; ++row) {
    for (int column = 0; column < art.width;) {
      const char key = art.rows[row][flip ? art.width - 1 - column : column];
      int run = 1;
      while (column + run < art.width && art.rows[row][flip ? art.width - 1 - column - run : column + run] == key) ++run;
      uint16_t color;
      if (art_color(palette, key, selfColor, color))
        target.fillRect(x + column * scale, y + row * scale, run * scale, scale, color);
      column += run;
    }
  }
}

// ---- マスコット（学科のマスコット: 青いコンデンサーにコウモリの羽） ----
// K 輪郭 / B 体 / G 底面 / P 頬 / p 光った頬 / V 羽 / N 足 / Y・O 金 / L・l 銀 / C・c 銅 / R 冠の宝石
// A・a 炎の縁取り。目・口・帯の点は共通の黒（X）、帯は共通の白（W）。
// 輪郭と足を黒にしないのは、筐体の黒い背景に体の外形と足が溶けるため。
constexpr ArtColor MASCOT_COLORS[] = {
  {'K', art_rgb(0x26307a)}, {'B', art_rgb(0x3f4bd0)}, {'G', art_rgb(0xc9c9d4)},
  {'P', art_rgb(0xf59ac0)}, {'p', art_rgb(0xff4f9a)}, {'V', art_rgb(0x5a5c8e)}, {'N', art_rgb(0xc9cad6)},
  {'Y', art_rgb(0xf5c518)}, {'O', art_rgb(0xb88a00)}, {'R', art_rgb(0xe0383e)},
  {'L', art_rgb(0xd6dae2)}, {'l', art_rgb(0x8e95a3)}, {'C', art_rgb(0xd08a4a)}, {'c', art_rgb(0x8a5326)},
  {'A', art_rgb(0xffd23f)}, {'a', art_rgb(0xff8a1e)},
};
constexpr ArtPalette MASCOT_PALETTE{MASCOT_COLORS, sizeof(MASCOT_COLORS) / sizeof(MASCOT_COLORS[0])};

// 体の右側面の白い帯の黒い点で「5600PF」の文字を表す。
constexpr const char* MASCOT_BODY[] = {
  ".KKKKKKKKKKKK.",
  "KBBBBBBBBBBWWK",
  "KBBBBBBBBBBXWK",
  "KBBBBBBBBBBWWK",
  "KBBBBBBBBBBXWK",
  "KPBBBBBBBPBWWK",
  "KBBBBBBBBBBXWK",
  "KBBBBBBBBBBWWK",
  "KGGGGGGGGGGGGK",
  ".KKKKKKKKKKKK.",
};
constexpr const char* const* MASCOT_BODY_FRAMES[] = {MASCOT_BODY};

// 顔は体と同じ位置に重ねる。目は縦長、口は「ω」。
constexpr const char* MASCOT_FACE_NORMAL[] = {
  "..............",
  "..............",
  "..X.....X.....",
  "..X.....X.....",
  "..............",
  "...X.X.X......",
  "....X.X.......",
};
constexpr const char* MASCOT_FACE_BLINK[] = {
  "..............",
  "..............",
  "..............",
  "..XX...XX.....",
  "..............",
  "...X.X.X......",
  "....X.X.......",
};
// 喜び: 目が「^ ^」。
constexpr const char* MASCOT_FACE_JOY[] = {
  "..............",
  "..............",
  "..X.....X.....",
  ".X.X...X.X....",
  "..............",
  "...X.X.X......",
  "....X.X.......",
};
// しょんぼり: 目が「× ×」。
constexpr const char* MASCOT_FACE_SAD[] = {
  "..............",
  ".X.X...X.X....",
  "..X.....X.....",
  ".X.X...X.X....",
  "..............",
  ".....X........",
  "....X.X.......",
};
enum class MascotFace : uint8_t { Normal, Blink, Joy, Sad };
constexpr const char* const* MASCOT_FACE_FRAMES[] = {
  MASCOT_FACE_NORMAL, MASCOT_FACE_BLINK, MASCOT_FACE_JOY, MASCOT_FACE_SAD,
};

// 羽ばたきは上・中・下・中の4コマ。右の羽は左の羽を左右反転して使う。
constexpr const char* MASCOT_WING_UP[] = {
  "K......",
  "KK.....",
  "KVK....",
  "KVVK...",
  "KVVVKK.",
  ".KVVVVV",
  "..KVKVV",
  "...K.KK",
  ".......",
  ".......",
  ".......",
  ".......",
  ".......",
};
constexpr const char* MASCOT_WING_MID[] = {
  ".......",
  ".......",
  ".......",
  ".......",
  "KK.....",
  "KVKK...",
  "KVVVKKK",
  "KVVVVVV",
  ".KVKVVV",
  "..K.KKK",
  ".......",
  ".......",
  ".......",
};
constexpr const char* MASCOT_WING_DOWN[] = {
  ".......",
  ".......",
  ".......",
  ".......",
  ".......",
  ".......",
  "....KKK",
  "...KVVV",
  "..KVVVV",
  ".KVVVVV",
  "KVVKVKK",
  "KVK.K..",
  "KK.....",
};
constexpr uint8_t MASCOT_WING_DOWN_FRAME = 2; // 羽を下ろすコマでは体を1画素上げ、弾むように見せる。
constexpr const char* const* MASCOT_WING_FRAMES[] = {MASCOT_WING_UP, MASCOT_WING_MID, MASCOT_WING_DOWN, MASCOT_WING_MID};

// 足のぶらぶら2コマ。
constexpr const char* MASCOT_LEGS_A[] = {
  "..N..N..",
  "..N...N.",
  "...N..N.",
};
constexpr const char* MASCOT_LEGS_B[] = {
  "..N..N..",
  "..N..N..",
  "..N..N..",
};
constexpr const char* const* MASCOT_LEGS_FRAMES[] = {MASCOT_LEGS_A, MASCOT_LEGS_B};

// 順位の冠（11×5）。最下段を体の上の輪郭に重ねて頭に載せる。銀・銅は金の色の文字を置き換えて作る。
constexpr const char* MASCOT_CROWN[] = {
  "Y....Y....Y",
  "YY..YYY..YY",
  "YYYYYWYYYYY",
  "YRYYYRYYYRY",
  "OOOOOOOOOOO",
};
constexpr const char* const* MASCOT_CROWN_FRAMES[] = {MASCOT_CROWN};
enum class Crown : uint8_t { None, Gold, Silver, Bronze };

constexpr ArtPart MASCOT_BODY_PART{MASCOT_BODY_FRAMES, 1, 14, 10, 0};
constexpr ArtPart MASCOT_FACE_PART{MASCOT_FACE_FRAMES, 4, 14, 7, 0};
constexpr ArtPart MASCOT_WING_PART{MASCOT_WING_FRAMES, 4, 7, 13, 140};
constexpr ArtPart MASCOT_LEGS_PART{MASCOT_LEGS_FRAMES, 2, 8, 3, 320};
constexpr ArtPart MASCOT_CROWN_PART{MASCOT_CROWN_FRAMES, 1, 11, 5, 0};
constexpr uint16_t MASCOT_BLINK_MS = 130;

// 枠は24×17（羽を広げた姿に冠の1行を足したもの）。部品はこの順に重ねる。
constexpr int MASCOT_WIDTH = 24, MASCOT_HEIGHT = 17;
enum class MascotLayer : uint8_t { Wing, Body, Face, Legs, Crown };
struct MascotLayerPlacement {
  MascotLayer layer;
  const ArtPart* part;
  int8_t x, y;
  bool mirror;
};
constexpr MascotLayerPlacement MASCOT_LAYERS[] = {
  {MascotLayer::Wing, &MASCOT_WING_PART, 0, 1, false},
  {MascotLayer::Wing, &MASCOT_WING_PART, 17, 1, true},
  {MascotLayer::Body, &MASCOT_BODY_PART, 5, 4, false},
  {MascotLayer::Face, &MASCOT_FACE_PART, 5, 4, false},
  {MascotLayer::Legs, &MASCOT_LEGS_PART, 8, 14, false},
  {MascotLayer::Crown, &MASCOT_CROWN_PART, 6, 0, false},
};

// 炎をまとう姿の縁取りは、枠の外側へ最大2画素はみ出す。
constexpr int MASCOT_AURA_MARGIN = 2;

struct MascotPose {
  uint8_t wing = 1;             // MASCOT_WING_FRAMESの番号
  MascotFace face = MascotFace::Normal;
  uint8_t legs = 0;
  Crown crown = Crown::None;
  bool blush = false;           // 頬を光らせる
  bool aura = false;            // 炎の縁取り
  uint8_t auraTick = 0;         // 縁取りの揺らぎ
};

// 部品を重ねた1コマ。縁取りのために枠の周りへMASCOT_AURA_MARGINずつ余白を持つ。
// rows_は自分のcells_を指すので、コピーさせない。
class MascotCanvas {
public:
  static constexpr int WIDTH = MASCOT_WIDTH + 2 * MASCOT_AURA_MARGIN;
  static constexpr int HEIGHT = MASCOT_HEIGHT + 2 * MASCOT_AURA_MARGIN;
  MascotCanvas(const MascotCanvas&) = delete;
  MascotCanvas& operator=(const MascotCanvas&) = delete;

  explicit MascotCanvas(const MascotPose& pose) {
    for (int y = 0; y < HEIGHT; ++y) {
      for (int x = 0; x < WIDTH; ++x) cells_[y][x] = ART_CLEAR;
      cells_[y][WIDTH] = '\0';
      rows_[y] = cells_[y];
    }
    for (const auto& placement : MASCOT_LAYERS) {
      if (placement.layer == MascotLayer::Crown && pose.crown == Crown::None) continue;
      const uint8_t index = placement.layer == MascotLayer::Wing ? pose.wing :
                            placement.layer == MascotLayer::Face ? static_cast<uint8_t>(pose.face) :
                            placement.layer == MascotLayer::Legs ? pose.legs : 0;
      stamp(placement.part->frame(index), placement.x, placement.y, placement.mirror, pose);
    }
    if (pose.aura) add_aura(pose.auraTick);
  }

  ArtFrame frame() const { return {rows_, WIDTH, HEIGHT}; }
  char at(int x, int y) const { return cells_[y][x]; }

private:
  void stamp(const ArtFrame& art, int left, int top, bool mirror, const MascotPose& pose) {
    for (int y = 0; y < art.height; ++y) {
      for (int x = 0; x < art.width; ++x) {
        char key = art.rows[y][mirror ? art.width - 1 - x : x];
        if (key == ART_CLEAR) continue;
        if (pose.blush && key == 'P') key = 'p';
        if (pose.crown == Crown::Silver) key = key == 'Y' ? 'L' : key == 'O' ? 'l' : key;
        if (pose.crown == Crown::Bronze) key = key == 'Y' ? 'C' : key == 'O' ? 'c' : key;
        cells_[MASCOT_AURA_MARGIN + top + y][MASCOT_AURA_MARGIN + left + x] = key;
      }
    }
  }

  bool opaque(int x, int y) const {
    return x >= 0 && x < WIDTH && y >= 0 && y < HEIGHT && cells_[y][x] != ART_CLEAR &&
           cells_[y][x] != 'A' && cells_[y][x] != 'a';
  }

  // 輪郭のすぐ外側をほぼ埋め、上下に2画素目をまばらに置いて、炎が揺らめくように見せる。
  void add_aura(uint8_t tick) {
    for (int y = 0; y < HEIGHT; ++y) {
      for (int x = 0; x < WIDTH; ++x) {
        if (cells_[y][x] != ART_CLEAR) continue;
        const bool near1 = opaque(x - 1, y) || opaque(x + 1, y) || opaque(x, y - 1) || opaque(x, y + 1);
        const bool near2 = !near1 && (opaque(x, y + 2) || opaque(x - 1, y + 1) || opaque(x + 1, y + 1));
        const uint32_t r = effect_hash(tick, x + 7, y + 11) % 100;
        if (near1 && r < 85) cells_[y][x] = r < 45 ? 'A' : 'a';
        else if (near2 && r < 35) cells_[y][x] = 'a';
      }
    }
  }

  char cells_[HEIGHT][WIDTH + 1];
  const char* rows_[HEIGHT];
};

// マスコットの枠の左上を(x, y)として描く。縁取りの余白はその外側に描く。
template <typename Target>
void draw_mascot(Target& target, const MascotPose& pose, int scale, int x, int y, bool flip) {
  const MascotCanvas canvas(pose);
  draw_art(target, canvas.frame(), MASCOT_PALETTE, scale,
           x - MASCOT_AURA_MARGIN * scale, y - MASCOT_AURA_MARGIN * scale, flip, 0);
}

// ---- 小物 ----
constexpr ArtColor ARROW_COLORS[] = {{'U', art_rgb(0x58d858)}};
constexpr ArtPalette ARROW_PALETTE{ARROW_COLORS, 1};
constexpr const char* UP_ARROW[] = {
  "..U..",
  ".UUU.",
  "UUUUU",
  ".UUU.",
  ".UUU.",
};
constexpr const char* const* UP_ARROW_FRAMES[] = {UP_ARROW};
constexpr ArtPart UP_ARROW_PART{UP_ARROW_FRAMES, 1, 5, 5, 0};

#endif // PIXEL_ART_HPP_
