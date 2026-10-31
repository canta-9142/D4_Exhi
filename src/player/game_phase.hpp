#ifndef GAME_PHASE_HPP_
#define GAME_PHASE_HPP_

#include <cstdint>

// Unknownは通信上のフェーズではなく、現在のフェーズが未取得であることを表す。
// 通信上のフェーズは、その筐体の段階を表す（Preparing=停止中、Idle=待機、Select=モード選択、Playing=ラウンド、Finished=結果）。
enum class GamePhase : uint8_t { Unknown, Preparing, Idle, Select, Playing, Finished };

#endif // GAME_PHASE_HPP_
