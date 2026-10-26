#pragma once

#include <cstddef>
#include <cstdint>
#include <string>

struct TestSerial {
  bool started = false;
  bool connected = true;
  int writable = 256;
  void (*beforeWrite)() = nullptr;
  std::string input, output;
  size_t position = 0;
  void setRxBufferSize(size_t) {}
  void setTxTimeoutMs(unsigned) {}
  void begin(unsigned) { started = true; }
  void setDebugOutput(bool) {}
  explicit operator bool() const { return started && connected; }
  int available() const { return input.size() - position; }
  int availableForWrite() const { return writable; }
  int read() { return static_cast<unsigned char>(input[position++]); }
  size_t write(const uint8_t* bytes, size_t count) {
    if (beforeWrite) beforeWrite();
    output.append(reinterpret_cast<const char*>(bytes), count);
    return count;
  }
};
inline TestSerial Serial;
inline uint32_t testMillis = 0;
inline uint32_t millis() { return testMillis; }
inline void delay(uint32_t ms) { testMillis += ms; }
struct TestESP {
  uint64_t getEfuseMac() const { return 0x001122334455; }
};
inline TestESP ESP;
