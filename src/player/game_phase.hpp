#ifndef GAME_PHASE_HPP_
#define GAME_PHASE_HPP_

#include <cstdint>

// Unknownは通信上のフェーズではなく、現在のフェーズが未取得であることを表す。
enum class GamePhase : uint8_t { Unknown, Preparing, Playing, Finished };

#endif // GAME_PHASE_HPP_
