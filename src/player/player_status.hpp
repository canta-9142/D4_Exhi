#ifndef PLAYER_STATUS_HPP_
#define PLAYER_STATUS_HPP_

#include <cstdint>

enum class ControlMode : uint8_t { Auto, Manual };

struct PlayerStatus {
  int32_t score = 0;
  uint8_t remaining = 0;
  bool ready = false;
  bool known = false;
};

#endif // PLAYER_STATUS_HPP_
