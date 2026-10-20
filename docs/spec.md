# ノードとパケット

## ノードIDと表示

ノードIDの定義は[node_id.hpp](../src/player/node_id.hpp)、表示名・色・隣接ノードは[node_config.hpp](../src/player/node_config.hpp)で管理する。

| 種別 | 内部ID | 表示名 | 表示色 |
| --- | --- | --- | --- |
| 未設定 | `None = 0` | `?` | 黒 |
| プレイヤー | `P1 = 1` | A | 赤 |
| プレイヤー | `P2 = 2` | B | オレンジ |
| プレイヤー | `P3 = 3` | C | 黄 |
| プレイヤー | `P4 = 4` | D | 緑 |
| プレイヤー | `P5 = 5` | E | シアン |
| プレイヤー | `P6 = 6` | F | 青 |
| プレイヤー | `P7 = 7` | G | 紫 |
| プレイヤー | `P8 = 8` | H | ピンク |
| ボット | `B1 = 9` | I | 白 |
| ボット | `B2 = 10` | J | 灰 |
| ボット | `B3 = 11` | K | 茶 |

`spec.md`のボット表記「1〜3」は、現行画面では「I〜K」に対応する。下部の4ボタンは、次の順番で左から描画する。

| 自ノード | 左端 | 左から2番目 | 左から3番目 | 右端 |
| --- | --- | --- | --- | --- |
| A | B | I | K | H |
| B | C | J | I | A |
| C | D | J | K | B |
| D | E | J | I | C |
| E | F | K | I | D |
| F | G | I | J | E |
| G | H | I | K | F |
| H | A | J | K | G |

# サーバー↔プレイヤーノード間通信インターフェース

## 基本仕様

- 通信経路: USB CDC
- データ形式: JSON Lines (UTF-8, 1行1オブジェクト)
- MCU: ESP32-S3, Arduino(FreeRTOS)
- JSONライブラリ: ArduinoJson 7
- サーバー: Radxa ROCK 3B, 中央集権型で、ゲームロジックを動かしながら全パケットを正本として管理する

## JSON形式

```json
{"type": "string","data": { ... }}
```

### メッセージ一覧

#### MCU → Server

- hello: 接続・識別
  - uid: ノード識別子、詳細未定

- send: パケットの転送を要求
  - pid: パケットID
  - to: 送信先ノード

- resync: キャッシュ再同期をサーバーに要求

#### Server → MCU

- config: ノードの設定
  - node: ノード

- reset: MCU側キャッシュ全更新
  - packets: 更新後のパケット

- push: MCU側キャッシュ末尾へのパケット追加
  - pid: パケットID
  - dst: 宛先ノード

- result: `send`の処理結果
  - ok: 成功判定(boolean)

### 通信シーケンス

```mermaid
sequenceDiagram
    participant M as MCU
    participant S as Server
    M->>S: HELLO
    S-->>M: CONFIG
    S-->>M: RESET
    Note over M: キャッシュ初期化
    S-->>M: PUSH
    Note over M: パケット追加
    M->>S: SEND
    S-->>M: PUSH（必要なら補充）
    S-->>M: RESULT OK
    Note over M: 先頭削除
    opt 不整合
        M->>S: RESYNC
        S-->>M: RESET
    end
```

# サーバー↔LEDノード間通信インターフェース

## 基本仕様

- 通信経路: USB CDC
- データ形式: 改行区切り独自プロトコル
- MCU: ESP32-C3, Arduino
- サーバー: 同

## メッセージ形式

メッセージ単位はLF区切り、命令・引数同士はスペース区切り

```txt
OPERATION OPTIONS
```

## メッセージ一覧

### LEDノード → Server

- HELLO:
  - UID

### Server → LEDノード

- CONFIG:
  - NODE

- LIGHT:
  - DESTINATION
  - DIRECTION (incoming|outgoing)
