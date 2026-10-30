#ifndef EFFECT_RANDOM_HPP_
#define EFFECT_RANDOM_HPP_

#include <cstdint>

// 演出の乱数。シードを指定でき、テストで結果を再現できる。
class Xorshift32 {
public:
  explicit Xorshift32(uint32_t seed = 1) : state_(seed ? seed : 1) {}
  uint32_t next() {
    state_ ^= state_ << 13;
    state_ ^= state_ >> 17;
    state_ ^= state_ << 5;
    return state_;
  }
  // 0〜bound-1。
  uint32_t below(uint32_t bound) { return next() % bound; }
private:
  uint32_t state_;
};

// 状態を持たない演出（火の粉・煙・炎の縁取り）の値を、シードと番号から決める。
inline uint32_t effect_hash(uint32_t seed, uint32_t a, uint32_t b = 0) {
  Xorshift32 rng(seed * 0x9e3779b1u ^ a * 0x85ebca6bu ^ b * 0xc2b2ae35u);
  rng.next();
  return rng.next();
}

#endif // EFFECT_RANDOM_HPP_
