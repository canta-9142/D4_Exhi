#ifndef PLAYER_INO_
#define PLAYER_INO_

#define GAME     "game.hpp"
#define FPS_DEMO "fps_demo.hpp"

// #define PLAYER_PACKET_DEMO
#if defined(PLAYER_FRAMEBUFFER_BENCH)
#include "frame_buffer_bench.hpp"
#elif defined(PLAYER_PLAYING_BENCH)
#include FPS_DEMO
#else
#include GAME
#endif

#endif // PLAYER_INO_
