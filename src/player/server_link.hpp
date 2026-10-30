#ifndef SERVER_LINK_HPP_
#define SERVER_LINK_HPP_

#include <ArduinoJson.h>
#include <algorithm>
#include <cstdio>
#include <cstring>
#include "packet.hpp"
#include "game_phase.hpp"
#include "player_status.hpp"
#include "ready_hold.hpp"

// PacketQueueと同じくゲーム側のloopだけから呼び出す。
class ServerLink {
public:
  static constexpr size_t MAX_LINE_BYTES = 32768;
  static constexpr uint32_t RETRY_MS = 1000;
  static constexpr uint32_t SEND_TIMEOUT_MS = 3000;
  static constexpr uint32_t STATUS_TIMEOUT_MS = 3000;
  static constexpr uint32_t SUMMARY_TIMEOUT_MS = 3000;
  static constexpr uint32_t READY_TIMEOUT_MS = 3000;
  static constexpr uint32_t RECEIVE_TIMEOUT_MS = 1000;
  static constexpr uint32_t DISCONNECT_GRACE_MS = 100;
  static constexpr unsigned MAX_GAIN = 15;

  // FIFOの置換・設定の無効化でtrueを返し、描画アニメーションも初期化する。
  template<class Port>
  bool poll(Port& port, const char* uid, PacketQueue& queue, NodeId& selfId, uint32_t nowMs) {
    if (!port) {
      if (!disconnectSeen_) {
        disconnectSeen_ = true;
        disconnectedMs_ = nowMs;
      }
      // Hardware CDCの接続判定は短時間falseになることがある。
      if (nowMs - disconnectedMs_ >= DISCONNECT_GRACE_MS) {
        const bool changed = state_ != State::AwaitConfig || queue.size() != 0;
        forget_config(queue, selfId);
        length_ = 0;
        dropping_ = false;
        txLength_ = 0;
        return changed;
      }
      return false;
    }
    disconnectSeen_ = false;
    flush_tx(port);

    bool reset = false;
    // 上限超過による破棄は、受信停止があってもLFまたは切断まで継続する。
    if (length_ && nowMs - receivedMs_ >= RECEIVE_TIMEOUT_MS) {
      length_ = 0;
      reset = forget_config(queue, selfId);
    }
    // 大きなRESETを受信中もボタンと描画スナップショットの更新へ戻る。
    for (size_t budget = 4096; budget && port.available(); --budget) {
      const int value = port.read();
      if (value < 0) break;
      receivedMs_ = nowMs;
      if (value == '\n') {
        if (length_ && line_[length_ - 1] == '\r') --length_;
        if (!dropping_ && length_) {
          reset |= receive(queue, selfId);
        }
        length_ = 0;
        dropping_ = false;
        if (state_ == State::AwaitConfig && resyncNeeded_) handshake(port, uid, nowMs);
      } else if (!dropping_) {
        // 上限ちょうどの行に続くCRも受け付ける（終端は上限に含めない）。
        if (length_ == sizeof(line_) || (length_ == MAX_LINE_BYTES && value != '\r')) {
          dropping_ = true;
          length_ = 0;
          reset |= forget_config(queue, selfId);
        } else {
          line_[length_++] = static_cast<char>(value);
        }
      }
    }

    if (fifo_synced() && phase_ != GamePhase::Unknown && statusSync_ == StatusSync::AwaitStatus) {
      if (!statusTimerStarted_) {
        statusTimerStarted_ = true;
        statusStartedMs_ = nowMs;
      } else if (nowMs - statusStartedMs_ >= STATUS_TIMEOUT_MS) {
        request_resync();
      }
    }
    if (fifo_synced() && phase_ == GamePhase::Finished && !preview_ && !summary_.known) {
      if (!summaryTimerStarted_) {
        summaryTimerStarted_ = true;
        summaryStartedMs_ = nowMs;
      } else if (nowMs - summaryStartedMs_ >= SUMMARY_TIMEOUT_MS) {
        request_resync();
      }
    }
    if (pending_ && nowMs - sentMs_ >= SEND_TIMEOUT_MS) request_resync();
    if (readyPending_ && nowMs - readySentMs_ >= READY_TIMEOUT_MS) request_resync();
    if ((!fifo_synced() || phase_ == GamePhase::Unknown) &&
        (resyncNeeded_ || nowMs - handshakeMs_ >= RETRY_MS)) {
      handshake(port, uid, nowMs);
    }
    return reset;
  }

  template<class Port>
  bool send(Port& port, const PacketQueue& queue, NodeId to, uint32_t nowMs) {
    if (!port || !fifo_synced() || preview_ || mode_ != ControlMode::Manual ||
        phase_ != GamePhase::Playing || counting_down() || pending_ || txLength_ || !queue.size() ||
        nextRid_ > UINT32_MAX) return false;
    const unsigned target = static_cast<unsigned>(to);
    if (target < 1 || target > static_cast<unsigned>(NodeId::B3)) return false;
    char message[160];
    const uint32_t rid = static_cast<uint32_t>(nextRid_);
    std::snprintf(message, sizeof(message),
                  "{\"type\":\"send\",\"data\":{\"sync\":\"%s\",\"rid\":%lu,\"pid\":%u,\"to\":%u}}\n",
                  sync_, static_cast<unsigned long>(rid), static_cast<unsigned>(queue.at(0)->id), target);
    // 書込失敗時も番号を再利用しない。欠番はプロトコルで許可されている。
    ++nextRid_;
    if (!write_line(port, message)) return false;
    pending_ = true;
    pendingRid_ = rid;
    pendingPid_ = queue.at(0)->id;
    sentMs_ = nowMs;
    return true;
  }

  bool fifo_synced() const { return state_ == State::Synced; }
  bool pending() const { return pending_; }
  GamePhase phase() const { return phase_; }
  ControlMode mode() const { return mode_; }
  const PlayerStatus& status() const { return status_; }
  // 前回取り出してから受け取った獲得点を古い順に返し、空にする。流すかどうかは表示側が決める。
  GainBatch take_gains() {
    const GainBatch gains = gains_;
    gains_ = {};
    return gains;
  }
  bool awaiting_status() const { return statusSync_ != StatusSync::Complete; }
  // カウントダウン中のFIFOは空のはずだが、念のため入力も無視する。
  // STATUSの取得待ちでは以前の値を使わず、SENDに追加の制限を設けない。
  bool counting_down() const { return status_.countdown && !awaiting_status(); }
  bool preview() const { return preview_; }
  const FinalSummary& summary() const { return summary_; }
  bool ready_pending() const { return readyPending_; }
  uint16_t ready_progress_ms() const { return readyPending_ ? ReadyHold::DURATION_MS : readyHold_.progress_ms(); }

  template<class Port>
  bool update_ready(Port& port, uint8_t pressedMask, uint32_t nowMs) {
    const bool enabled = port && !preview_ && fifo_synced() && phase_ == GamePhase::Preparing &&
                         mode_ == ControlMode::Manual && !awaiting_status() && status_.known &&
                         !status_.ready && !readyPending_;
    if (!readyHold_.update(pressedMask, enabled, nowMs)) return false;
    char message[96];
    std::snprintf(message, sizeof(message), "{\"type\":\"ready\",\"data\":{\"sync\":\"%s\"}}\n", sync_);
    if (!write_line(port, message)) {
      request_resync();
      return false;
    }
    readyPending_ = true;
    readySentMs_ = nowMs;
    return true;
  }

private:
  enum class State : uint8_t { AwaitConfig, AwaitReset, Synced };
  enum class StatusSync : uint8_t { AwaitReset, AwaitStatus, Complete };

  template<class Port>
  void flush_tx(Port& port) {
    if (!txLength_) return;
    const int available = port.availableForWrite();
    if (available <= 0) return;
    const size_t count = std::min(txLength_, static_cast<size_t>(available));
    const size_t written = port.write(reinterpret_cast<const uint8_t*>(txRemaining_), count);
    txLength_ -= written;
    std::memmove(txRemaining_, txRemaining_ + written, txLength_);
  }

  template<class Port>
  bool write_line(Port& port, const char* message) {
    const size_t size = std::strlen(message);
    if (txLength_ || size > sizeof(txRemaining_) ||
        port.availableForWrite() < static_cast<int>(size)) return false;
    const size_t written = port.write(reinterpret_cast<const uint8_t*>(message), size);
    if (!written) return false;
    // 途中まで送れた行は、設定変更や再同期があっても次の行より先に完送する。
    txLength_ = size - written;
    std::memcpy(txRemaining_, message + written, txLength_);
    return true;
  }

  template<class Port>
  void handshake(Port& port, const char* uid, uint32_t nowMs) {
    char message[96];
    const bool hello = state_ == State::AwaitConfig;
    if (hello) {
      std::snprintf(message, sizeof(message), "{\"type\":\"hello\",\"data\":{\"uid\":\"%s\"}}\n", uid);
    } else {
      std::snprintf(message, sizeof(message), "{\"type\":\"resync\",\"data\":{}}\n");
    }
    if (write_line(port, message)) {
      if (!hello) request_resync();
      handshakeMs_ = nowMs;
      resyncNeeded_ = false;
    }
  }

  void request_resync() {
    if (state_ != State::AwaitConfig) state_ = State::AwaitReset;
    pending_ = false;
    resyncNeeded_ = true;
    statusSync_ = StatusSync::AwaitReset;
    statusTimerStarted_ = false;
    summaryTimerStarted_ = false;
    allowIntro_ = false;
    summary_.animate = false;
    cancel_ready();
  }

  void cancel_ready() {
    readyHold_.reset();
    readyPending_ = false;
  }

  bool forget_config(PacketQueue& queue, NodeId& selfId) {
    queue.clear();
    selfId = NodeId::None;
    state_ = State::AwaitConfig;
    phase_ = GamePhase::Unknown;
    mode_ = ControlMode::Auto;
    status_ = {};
    gains_ = {};
    preview_ = false;
    summary_ = {};
    sync_[0] = '\0';
    request_resync();
    return true;
  }

  static bool valid_sync(JsonVariantConst value) {
    if (!value.is<const char*>() || value.as<JsonString>().size() != 32) return false;
    const char* sync = value.as<const char*>();
    for (size_t i = 0; i < 32; ++i) {
      if (!((sync[i] >= '0' && sync[i] <= '9') || (sync[i] >= 'A' && sync[i] <= 'F'))) return false;
    }
    return true;
  }

  // ベストスコアは0〜2147483647の整数かnull。nullはNO_BESTで持つ。欠落は不正。
  static bool parse_best(JsonVariantConst value, int32_t& best) {
    if (!value.isUnbound() && value.isNull()) {
      best = FinalSummary::NO_BEST;
      return true;
    }
    if (!value.is<int32_t>() || value.as<int32_t>() < 0) return false;
    best = value.as<int32_t>();
    return true;
  }

  // *Mineがtrueなら、対象者の結果で、そのベストがこの結果のスコアと等しい。
  static bool parse_mine(JsonVariantConst value, bool eligible, int32_t score, int32_t best, bool& mine) {
    if (!value.is<bool>()) return false;
    mine = value.as<bool>();
    return !mine || (eligible && best != FinalSummary::NO_BEST && best == score);
  }

  // summaryのベストスコアの項目を型・範囲・組合せまで検証してから返す。
  static bool parse_bests(JsonObjectConst data, bool eligible, int32_t score, FinalSummary& bests) {
    if (!parse_best(data["gameBest"], bests.gameBest) || !data["gameBestNodes"].is<JsonArrayConst>()) return false;
    // 1〜8を昇順に重複なく並べる。gameBestがnullなら空、整数なら1個以上。
    const JsonArrayConst nodes = data["gameBestNodes"].as<JsonArrayConst>();
    if ((bests.gameBest == FinalSummary::NO_BEST) != (nodes.size() == 0)) return false;
    unsigned previous = 0;
    for (JsonVariantConst node : nodes) {
      if (!node.is<uint8_t>() || node.as<unsigned>() <= previous || node.as<unsigned>() > 8) return false;
      previous = node.as<unsigned>();
      bests.gameBestNodes |= 1u << (previous - 1);
    }
    // hourBestStartはhourBestと一緒にnull、整数なら0〜23。
    const JsonVariantConst start = data["hourBestStart"];
    if (!parse_best(data["hourBest"], bests.hourBest)) return false;
    if (bests.hourBest == FinalSummary::NO_BEST) {
      if (start.isUnbound() || !start.isNull()) return false;
    } else {
      if (!start.is<uint8_t>() || start.as<unsigned>() > 23) return false;
      bests.hourBestStart = start.as<int8_t>();
    }
    return parse_mine(data["hourBestMine"], eligible, score, bests.hourBest, bests.hourBestMine) &&
           parse_best(data["allTimeBest"], bests.allTimeBest) &&
           parse_mine(data["allTimeBestMine"], eligible, score, bests.allTimeBest, bests.allTimeBestMine);
  }

  // rankは必須で、nullか1〜8の整数。
  static bool valid_rank(JsonVariantConst value) {
    return (!value.isUnbound() && value.isNull()) ||
           (value.is<uint8_t>() && value.as<unsigned>() >= 1 && value.as<unsigned>() <= 8);
  }

  // gainsは必須で、0〜16個の1〜15の整数の配列。
  static bool valid_gains(JsonVariantConst value) {
    if (!value.is<JsonArrayConst>() || value.as<JsonArrayConst>().size() > GainBatch::CAPACITY) return false;
    for (JsonVariantConst gain : value.as<JsonArrayConst>()) {
      if (!gain.is<uint8_t>() || gain.as<unsigned>() < 1 || gain.as<unsigned>() > MAX_GAIN) return false;
    }
    return true;
  }

  static GamePhase parse_phase(JsonVariantConst value) {
    if (!value.is<const char*>()) return GamePhase::Unknown;
    const JsonString phase = value.as<JsonString>();
    if (phase == "Preparing") return GamePhase::Preparing;
    if (phase == "Playing") return GamePhase::Playing;
    if (phase == "Finished") return GamePhase::Finished;
    return GamePhase::Unknown;
  }

  static bool valid_packet(JsonVariantConst value) {
    return value.is<JsonObjectConst>() && value["pid"].is<uint16_t>() &&
           value["dst"].is<uint8_t>() && value["dst"].as<unsigned>() >= 1 &&
           value["dst"].as<unsigned>() <= static_cast<unsigned>(NodeId::B3);
  }

  static Packet packet_from(JsonVariantConst value) {
    return {value["pid"].as<uint16_t>(), static_cast<NodeId>(value["dst"].as<uint8_t>())};
  }

  static bool apply_reset(JsonArrayConst packets, PacketQueue& queue) {
    if (packets.isNull() || packets.size() > PacketQueue::capacity()) return false;

    // 先に全要素を検証し、不正なRESETで既存キャッシュを失わないようにする。
    std::array<uint16_t, PacketQueue::capacity()> ids;
    size_t count = 0;
    for (JsonVariantConst packet : packets) {
      if (!valid_packet(packet)) return false;
      ids[count++] = packet["pid"].as<uint16_t>();
    }
    std::sort(ids.begin(), ids.begin() + count);
    if (std::adjacent_find(ids.begin(), ids.begin() + count) != ids.begin() + count) return false;

    queue.clear();
    for (JsonVariantConst value : packets) queue.push(packet_from(value));
    return true;
  }

  // ArduinoJsonはオブジェクト終端で解析を止めるため、残りが空白だけかも確認する。
  struct LineReader {
    const char* data;
    size_t size;
    size_t position = 0;
    int read() { return position < size ? static_cast<unsigned char>(data[position++]) : -1; }
    size_t readBytes(char* buffer, size_t count) {
      count = std::min(count, size - position);
      std::memcpy(buffer, data + position, count);
      position += count;
      return count;
    }
    bool finished() {
      for (int ch; (ch = read()) >= 0;) {
        if (ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n') return false;
      }
      return true;
    }
  };

  static bool valid_utf8(const char* data, size_t size) {
    for (size_t i = 0; i < size;) {
      const uint8_t lead = static_cast<uint8_t>(data[i++]);
      if (lead < 0x80) continue;
      size_t remaining;
      uint32_t codepoint;
      uint32_t minimum;
      if (lead >= 0xC2 && lead <= 0xDF) {
        remaining = 1;
        codepoint = lead & 0x1F;
        minimum = 0x80;
      } else if (lead >= 0xE0 && lead <= 0xEF) {
        remaining = 2;
        codepoint = lead & 0x0F;
        minimum = 0x800;
      } else if (lead >= 0xF0 && lead <= 0xF4) {
        remaining = 3;
        codepoint = lead & 0x07;
        minimum = 0x10000;
      } else {
        return false;
      }
      if (size - i < remaining) return false;
      for (size_t n = 0; n < remaining; ++n) {
        const uint8_t next = static_cast<uint8_t>(data[i++]);
        if ((next & 0xC0) != 0x80) return false;
        codepoint = (codepoint << 6) | (next & 0x3F);
      }
      if (codepoint < minimum || codepoint > 0x10FFFF ||
          (codepoint >= 0xD800 && codepoint <= 0xDFFF)) return false;
    }
    return true;
  }

  // ArduinoJsonが許す引用符なしのキー・単引用符・拡張数値などを除く。
  // オブジェクトや配列の構造検証と値の復号はArduinoJsonに任せる。
  static bool valid_json_tokens(const char* data, size_t size) {
    const auto digit = [](char ch) { return ch >= '0' && ch <= '9'; };
    bool quoted = false;
    for (size_t i = 0; i < size;) {
      const char ch = data[i++];
      if (ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n') continue;
      if (ch == ':') {
        if (!quoted) return false;
        quoted = false;
        continue;
      }
      quoted = false;
      if (ch == '{' || ch == '}' || ch == '[' || ch == ']' || ch == ',') continue;
      if (ch == '"') {
        bool closed = false;
        bool pendingHighSurrogate = false;
        while (i < size) {
          const unsigned char value = data[i++];
          // 上位サロゲートの直後には下位サロゲートの\uXXXXだけを許可する。
          if (pendingHighSurrogate && (value != '\\' || i == size || data[i] != 'u')) return false;
          if (value == '"') { closed = true; break; }
          if (value < 0x20) return false;
          if (value != '\\') continue;
          if (i == size) return false;
          const char escape = data[i++];
          if (escape == 'u') {
            uint16_t codeunit = 0;
            for (int n = 0; n < 4; ++n) {
              if (i == size) return false;
              const char hex = data[i++];
              unsigned digitValue;
              if (digit(hex)) digitValue = hex - '0';
              else if (hex >= 'a' && hex <= 'f') digitValue = hex - 'a' + 10;
              else if (hex >= 'A' && hex <= 'F') digitValue = hex - 'A' + 10;
              else return false;
              codeunit = static_cast<uint16_t>((codeunit << 4) | digitValue);
            }
            if (pendingHighSurrogate) {
              if (codeunit < 0xDC00 || codeunit > 0xDFFF) return false;
              pendingHighSurrogate = false;
            } else if (codeunit >= 0xD800 && codeunit <= 0xDBFF) {
              pendingHighSurrogate = true;
            } else if (codeunit >= 0xDC00 && codeunit <= 0xDFFF) {
              return false;
            }
          } else if (!std::strchr("\"\\/bfnrt", escape) || !escape) {
            return false;
          }
        }
        if (!closed) return false;
        quoted = true;
        continue;
      }
      if (ch == '-' || digit(ch)) {
        --i;
        if (data[i] == '-') ++i;
        if (i == size || !digit(data[i])) return false;
        if (data[i++] != '0') while (i < size && digit(data[i])) ++i;
        if (i < size && data[i] == '.') {
          ++i;
          if (i == size || !digit(data[i])) return false;
          while (i < size && digit(data[i])) ++i;
        }
        if (i < size && (data[i] == 'e' || data[i] == 'E')) {
          ++i;
          if (i < size && (data[i] == '+' || data[i] == '-')) ++i;
          if (i == size || !digit(data[i])) return false;
          while (i < size && digit(data[i])) ++i;
        }
      } else {
        const char* literal = ch == 't' ? "true" : ch == 'f' ? "false" : ch == 'n' ? "null" : nullptr;
        if (!literal) return false;
        --i;
        const size_t count = std::strlen(literal);
        if (size - i < count || std::memcmp(data + i, literal, count)) return false;
        i += count;
      }
      // 隣接するトークン（01、1.e2、truexなど）を数値やキーとして解釈させない。
      if (i < size && !std::strchr(" \t\r\n,]}:", data[i])) return false;
      if (i < size && data[i] == '\0') return false;
    }
    return true;
  }

  bool receive(PacketQueue& queue, NodeId& selfId) {
    if (!valid_utf8(line_, length_) || !valid_json_tokens(line_, length_)) {
      return forget_config(queue, selfId);
    }
    JsonDocument message;
    LineReader reader{line_, length_};
    const auto error = deserializeJson(message, reader, DeserializationOption::NestingLimit(4));
    if (error || !reader.finished()) return forget_config(queue, selfId);
    const bool isConfig = message["type"].is<const char*>() && message["type"].as<JsonString>() == "config";
    if (!message.is<JsonObject>() || !message["type"].is<const char*>() ||
        !message["data"].is<JsonObject>()) {
      if (isConfig) return forget_config(queue, selfId);
      request_resync();
      return false;
    }
    const char* type = message["type"];
    if (message["type"].as<JsonString>().size() != std::strlen(type)) {
      request_resync();
      return false;
    }
    const JsonObjectConst data = message["data"];

    if (std::strcmp(type, "identify") == 0) {
      return forget_config(queue, selfId);
    } else if (std::strcmp(type, "config") == 0) {
      if (!data["node"].is<uint8_t>() || data["node"].as<unsigned>() < 1 ||
          data["node"].as<unsigned>() > 8 || !data["mode"].is<const char*>()) {
        return forget_config(queue, selfId);
      }
      const JsonString mode = data["mode"].as<JsonString>();
      if (mode != "manual" && mode != "auto") return forget_config(queue, selfId);
      if (!data["preview"].is<bool>() || (data["preview"].as<bool>() && mode != "auto")) {
        return forget_config(queue, selfId);
      }
      forget_config(queue, selfId);
      preview_ = data["preview"].as<bool>();
      selfId = static_cast<NodeId>(data["node"].as<uint8_t>());
      mode_ = mode == "manual" ? ControlMode::Manual : ControlMode::Auto;
      state_ = State::AwaitReset;
      return true;
    } else if (std::strcmp(type, "phase") == 0) {
      const GamePhase phase = parse_phase(data["phase"]);
      if (phase == GamePhase::Unknown) {
        request_resync();
      } else if (state_ != State::AwaitConfig) {
        if (phase_ != phase) {
          // 直前のpollのキャッシュではなく、この通知時点の同期状態で判定する。
          allowIntro_ = phase_ == GamePhase::Playing && phase == GamePhase::Finished &&
                        fifo_synced() && !preview_ && !awaiting_status();
          summary_ = {};
          summaryTimerStarted_ = false;
          cancel_ready();
        }
        phase_ = phase;
      }
    } else if (std::strcmp(type, "status") == 0) {
      if (!data["score"].is<int32_t>() || data["score"].as<int32_t>() < 0 ||
          !data["remaining"].is<uint8_t>() || data["remaining"].as<unsigned>() > 120 ||
          !data["ready"].is<bool>() ||
          !data["countdown"].is<uint8_t>() || data["countdown"].as<unsigned>() > 3 ||
          !valid_rank(data["rank"]) || !valid_gains(data["gains"])) {
        request_resync();
      } else if (state_ != State::AwaitConfig) {
        // comboは後の作業で表示する。今は読まない。
        status_ = {data["score"].as<int32_t>(), data["remaining"].as<uint8_t>(),
                   data["ready"].as<bool>(), true, data["countdown"].as<uint8_t>(),
                   data["rank"].isNull() ? uint8_t(0) : data["rank"].as<uint8_t>()};
        for (JsonVariantConst gain : data["gains"].as<JsonArrayConst>()) gains_.push(gain.as<uint8_t>());
        if (status_.ready) cancel_ready();
        if (statusSync_ == StatusSync::AwaitStatus) {
          statusSync_ = StatusSync::Complete;
          statusTimerStarted_ = false;
        }
      }
    } else if (std::strcmp(type, "summary") == 0) {
      const bool eligible = data["eligible"].as<bool>();
      const bool validRank = eligible ?
          data["rank"].is<uint8_t>() && data["rank"].as<unsigned>() >= 1 && data["rank"].as<unsigned>() <= 8 :
          !data["rank"].isUnbound() && data["rank"].isNull();
      // topPercentは必須。対象外ではnullだけ、対象者ではnullまたは1〜50。
      const JsonVariantConst top = data["topPercent"];
      const bool validTop = (!top.isUnbound() && top.isNull()) ||
          (eligible && top.is<uint8_t>() && top.as<unsigned>() >= 1 && top.as<unsigned>() <= 50);
      // bottomHalfも必須。trueは下位50%の対象者だけで、topPercentはnull。
      const bool validBottom = data["bottomHalf"].is<bool>() &&
          (!data["bottomHalf"].as<bool>() || (eligible && top.isNull()));
      FinalSummary result;
      if (!valid_sync(data["sync"]) || !data["score"].is<int32_t>() || data["score"].as<int32_t>() < 0 ||
          !data["eligible"].is<bool>() || !validRank || !validTop || !validBottom ||
          !parse_bests(data, eligible, data["score"].as<int32_t>(), result)) {
        request_resync();
      } else if (fifo_synced() && phase_ == GamePhase::Finished && !preview_ &&
                 std::strcmp(sync_, data["sync"].as<const char*>()) == 0) {
        // 同じ世代の再通知では、演出の要否と世代番号を最初の通知のまま保つ。
        const bool first = !summary_.known;
        result.revision = first ? ++summaryRevision_ : summary_.revision;
        result.animate = first ? allowIntro_ : summary_.animate;
        result.score = data["score"].as<int32_t>();
        result.rank = eligible ? data["rank"].as<uint8_t>() : uint8_t(0);
        result.eligible = eligible;
        result.known = true;
        result.topPercent = top.isNull() ? uint8_t(0) : top.as<uint8_t>();
        result.bottomHalf = data["bottomHalf"].as<bool>();
        summary_ = result;
        summaryTimerStarted_ = false;
        allowIntro_ = false;
      }
    } else if (std::strcmp(type, "reset") == 0) {
      if (state_ == State::AwaitConfig) return false;
      if (!valid_sync(data["sync"]) || !apply_reset(data["packets"].as<JsonArrayConst>(), queue)) {
        request_resync();
        return false;
      }
      if (std::strcmp(sync_, data["sync"].as<const char*>()) != 0) summary_ = {};
      if (phase_ == GamePhase::Finished) {
        allowIntro_ = false;
        summary_.animate = false;
      }
      std::memcpy(sync_, data["sync"].as<const char*>(), sizeof(sync_));
      cancel_ready();
      if (statusSync_ == StatusSync::AwaitReset) statusSync_ = StatusSync::AwaitStatus;
      state_ = State::Synced;
      pending_ = resyncNeeded_ = false;
      return true;
    } else if (std::strcmp(type, "push") == 0) {
      if (!valid_packet(data)) {
        request_resync();
        return false;
      }
      if (!fifo_synced()) return false;
      const Packet packet = packet_from(data);
      for (size_t i = 0; i < queue.size(); ++i) {
        if (queue.at(i)->id == packet.id) {
          request_resync();
          return false;
        }
      }
      if (!queue.push(packet)) request_resync();
    } else if (std::strcmp(type, "result") == 0) {
      if (!valid_sync(data["sync"]) || !data["rid"].is<uint32_t>() || !data["ok"].is<bool>()) {
        request_resync();
        return false;
      }
      if (!fifo_synced() || !pending_ || std::strcmp(sync_, data["sync"].as<const char*>()) != 0 ||
          data["rid"].as<uint32_t>() != pendingRid_) return false;
      if (!queue.size() || queue.at(0)->id != pendingPid_) {
        request_resync();
        return false;
      }
      if (data["ok"].as<bool>()) queue.pop();
      pending_ = false;
    } else {
      request_resync();
    }
    return false;
  }

  char line_[MAX_LINE_BYTES + 1]{};
  char txRemaining_[160]{};
  size_t txLength_ = 0;
  size_t length_ = 0;
  bool dropping_ = false;
  State state_ = State::AwaitConfig;
  GamePhase phase_ = GamePhase::Unknown;
  ControlMode mode_ = ControlMode::Auto;
  PlayerStatus status_{};
  GainBatch gains_{};
  bool preview_ = false;
  FinalSummary summary_{};
  uint32_t summaryRevision_ = 0;
  bool allowIntro_ = false;
  bool summaryTimerStarted_ = false;
  uint32_t summaryStartedMs_ = 0;
  StatusSync statusSync_ = StatusSync::AwaitReset;
  bool statusTimerStarted_ = false;
  uint32_t statusStartedMs_ = 0;
  char sync_[33]{};
  bool pending_ = false;
  bool resyncNeeded_ = true;
  bool disconnectSeen_ = false;
  uint16_t pendingPid_ = 0;
  uint32_t pendingRid_ = 0;
  // 32bit上限の次を保持して送信を停止する。再識別・再同期でも巻き戻さない。
  uint64_t nextRid_ = 0;
  uint32_t sentMs_ = 0;
  uint32_t handshakeMs_ = 0;
  uint32_t receivedMs_ = 0;
  uint32_t disconnectedMs_ = 0;
  ReadyHold readyHold_;
  bool readyPending_ = false;
  uint32_t readySentMs_ = 0;
};

#endif // SERVER_LINK_HPP_
