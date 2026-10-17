#ifndef NODE_ID_HPP_
#define NODE_ID_HPP_

#include <cstdint>

enum class NodeId : uint8_t {
  None = 0,
  P1 = 1, P2, P3, P4, P5, P6, P7, P8,
  B1 = 9, B2, B3
};

#endif // NODE_ID_HPP_
