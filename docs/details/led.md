# LEDファームウェア

[LEDノード仕様](../spec/led.md)に従うESP32-C3用のArduinoスケッチ。
通常ビルドではUSB CDCから命令を受け取り、`LED_PACKET_DEMO`を定義すると従来の単一テープのデモを動かす。

## 配線とLED数

[node_config.hpp](../../src/led/node_config.hpp)の`ROUTES`に制御ノード・隣接ノード・GPIO・LED数をまとめている。
現時点ではタワーを含む各テープを1個で仮固定している。配置確定後は各行の`count`を実測値に変更する。
経路テープは先頭のLEDを制御ノード側とし、`outgoing`で先頭から末尾へ、`incoming`で末尾から先頭へ流す。
ノードIDの表記は`P1`〜`P8`および`B1`〜`B3`。`CONFIG`で指定できる制御ノードは配線表に存在する`B1`・`B2`・`B3`・`P1`・`P3`・`P5`・`P7`。

## 通信と描画

起動時と再接続時に、eFuse MAC由来の12桁の16進UIDで`HELLO UID`を送信する。
`CONFIG`を受け取るまでは1秒ごとに再送する。USB未接続判定が100ms継続すると設定を破棄して全出力を消灯する。

```text
CONFIG B1
BUFFER 2
LIGHT P2 P8 outgoing
LIGHT P1 P4 incoming
CLEAR
```

この例では、タワーを緑にし、P2へ向かうテープにP8の色を外向きに、P1へ向かうテープにP4の色を内向きに流す。
色はプレイヤーファームウェアの`nodeVisuals`と同じRGB565値から変換する。
10msごとに1LED進め、先頭を含む10サンプルの輝度を`255,128,64,32,16,8,4,2,1,0`とする。
重なった演出はRGB各成分を加算し、255で飽和させる。実際の出力時間が延びても経過時間から位置を計算する。
LED数が1個の間は移動方向の見た目は同じになり、減衰と色の加算を確認できる。

`BUFFER COUNT`はBot制御ノードのみで有効で、`COUNT`は0〜4。0〜1は青、2は緑、3は黄、4は赤。
`CLEAR`は演出とタワーを消灯し、ノード設定を保持する。
`CONFIG`の再受信でも演出とタワーを消灯する。タワーは次の`BUFFER`を受けるまで消灯したままとする。

不正な命令・余分な引数・未接続経路・タワーへの`LIGHT`は状態を変更せず無視する。
受信行はLF区切りで最大96バイト、CRLFも受理する。長過ぎる行、NULや非ASCII文字を含む行、受信が1秒以上途切れた行は次のLFまで捨てる。
同時に保持する経路演出は制御ノード全体で128本までとし、上限超過時は新しい`LIGHT`を無視する。終了した演出の領域は再利用する。

## ビルド

ESP32 Arduinoコア3.3.12、Adafruit NeoPixel 1.15.5で確認。
USB CDC On Bootを有効にし、通信へデバッグログを混ぜない。

```sh
nix develop -c arduino-cli lib install 'Adafruit NeoPixel@1.15.5'
nix develop -c arduino-cli compile \
  --fqbn esp32:esp32:esp32c3:CDCOnBoot=cdc,DebugLevel=none \
  --jobs 2 --build-path /tmp/d4-led-cdc-build src/led
```

従来のデモはGPIO 1、60LED、20ms周期、尾20LEDの動作をそのまま残している。
[led.ino](../../src/led/led.ino)の`LED_PACKET_DEMO`を有効にするか、ビルド時に次のように指定する。

```sh
nix develop -c arduino-cli compile \
  --fqbn esp32:esp32:esp32c3:CDCOnBoot=cdc,DebugLevel=none \
  --build-property compiler.cpp.extra_flags=-DLED_PACKET_DEMO \
  --jobs 2 --build-path /tmp/d4-led-demo-build src/led
```

## テスト

[led_controller.cpp](../../tests/led_controller.cpp)では実際の通信・描画処理をホスト上で動かし、識別、全ノードのGPIO設定、方向、減衰、飽和加算、タワー、消灯、不正入力、切断、時刻の周回を検証する。

```sh
nix develop -c bash -c '
  c++ -std=c++17 -Wall -Wextra -Werror -fsanitize=address,undefined \
    tests/led_controller.cpp -o /tmp/d4-led-controller-test &&
  /tmp/d4-led-controller-test
'
```

実機でのUSB通信・発光・10ms周期の維持は未確認。テープの長さを確定した後、最大接続本数と実際のLED数で出力時間を確認する。
