# LEDノードとLEDテープの対応

(後日追記)

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
  