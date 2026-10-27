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
起動時は通信開始前に配線表の全GPIOへ消灯データを送り、LED電源を保ったMCU再起動でも前の表示を消す。
NODEが未確定なので、GPIOごとに配線表の最大LED数を使用する。出力初期化に失敗した場合は再試行する。
`CONFIG`を受け取るまでは1秒ごとに再送する。USB未接続判定が100ms継続すると設定を破棄して全出力を消灯する。
切断確定中はCDCの受信キューも破棄し、旧接続の命令を再接続後へ持ち越さない。100ms未満の切断では受信状態・キューを保持する。
起動・再接続直後は最初の`HELLO`を送信してから受信命令を処理する。送信バッファ不足の場合は、その送信まで新しい入力の処理を保留する。
共通JSONの`{"type":"identify","data":{}}`は全状態で受け付け、設定・表示・演出・受信状態を初期化して毎回直ちに`HELLO UID`を返す。
設定変更・無効化時の出力処理は`poll()`のコールバックで同期的に行い、旧出力の消灯を完了してから応答を送る。
送信バッファ不足時は応答件数を保持し、後続の`CONFIG`で設定済みになっても送信を再試行する。切断確定時には旧接続の未送信応答を破棄する。
出力初期化の失敗時は`output_failed()`で未設定へ戻し、同じ接続の未送信応答と受信行を維持する。未送信応答がなければ復旧用の`HELLO`を1件保留する。
設定済みの通常の無通信にはタイムアウトを設けず、タワーの表示を維持する。

```text
CONFIG B1 false
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
`CONFIG NODE PREVIEW`は両引数を必須とし、PREVIEWは小文字の`true`・`false`だけを受理する。
`CLEAR`は演出とタワーを消灯し、NODE・PREVIEWを保持する。
`CONFIG`の再受信でも演出とタワーを初期化する。通常表示（preview=false）のタワーは次の`BUFFER`まで消灯を保持する。

`CONFIG NODE true`では、MCUが適用時を起点とする1秒周期で確認表示を描画する。
GPIO0・1の経路にはNODE自身の色を外向きに流し、Bot用GPIO0のタワーは赤く点灯する。
周期内500msで全出力を消灯する。遅延時は経過時間から現在の周期内の位置を求め、過去の周期をまとめて再生しない。
確認中の通常の`LIGHT`・`BUFFER`は適用しない。`CLEAR`は現在の周期を消灯し、起点を変えず次の周期で再開する。
確認終了は`CONFIG NODE false`、未割当基板は`identify`で行う。`identify`・切断・受信異常では確認モードも解除する。

`LIGHT`・`BUFFER`・`CLEAR`の不正な引数や逆方向の`HELLO`は状態を変更せず無視する。
不正な`CONFIG`・未知命令・不正なJSONは全出力を消灯して未設定へ戻し、`HELLO UID`による再識別を行う。行末で判明したエラーでは次の行を通常どおり処理する。
受信行は終端を除いて最大96バイトとし、LF・CRLF、前後や区切りのスペース・タブを受理する。
長過ぎる行、NUL・非ASCII・許可されていない制御文字を含む行、最後の受信から1秒以上途切れた部分行も同様に再識別へ移るが、当該行は次のLFまで捨てる。
行破棄中も`HELLO`を1秒ごとに再送し、未設定中の追加エラーで再送周期を巻き戻さない。
同時に保持する経路演出は制御ノード全体で128本までとし、上限超過時は新しい`LIGHT`を無視する。終了した演出の領域は再利用する。

## ビルド

ESP32 Arduinoコア3.3.12、Adafruit NeoPixel 1.15.5、ArduinoJson 7.4.3で確認。
USB CDC On Bootを有効にし、通信へデバッグログを混ぜない。

```sh
nix develop -c arduino-cli lib install 'Adafruit NeoPixel@1.15.5'
nix develop -c arduino-cli lib install 'ArduinoJson@7.4.3'
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

[led_controller.cpp](../../tests/led_controller.cpp)では実際の通信・描画処理をホスト上で動かし、識別、全ノードのGPIO設定、方向、減衰、飽和加算、タワー、確認周期と解除、消灯、不正入力、切断、時刻の周回を検証する。
GCC 16でのArduinoJson内の最適化時警告を避けるため、ホストテストはNixのhardeningを無効化した非最適化ビルドで実行する。

```sh
nix develop -c bash -c '
  NIX_HARDENING_ENABLE="" c++ -O0 -std=c++17 -Wall -Wextra -Werror \
    -fsanitize=address,undefined -I "$HOME/Arduino/libraries/ArduinoJson/src" \
    tests/led_controller.cpp -o /tmp/d4-led-controller-test &&
  /tmp/d4-led-controller-test
'
```

[led_firmware.cpp](../../tests/led_firmware.cpp)はArduino・NeoPixelの代替実装で実際の`setup()`・`loop()`を動かし、起動時の全GPIO消灯、初期化失敗からの再試行、消灯と識別応答の順序、設定失敗時の応答保持と通常設定への移行を確認する。

```sh
nix develop -c bash -c '
  NIX_HARDENING_ENABLE="" c++ -O0 -std=c++17 -Wall -Wextra -Werror \
    -fsanitize=address,undefined -I "$HOME/Arduino/libraries/ArduinoJson/src" \
    -I tests/led_stubs tests/led_firmware.cpp -o /tmp/d4-led-firmware-test &&
  /tmp/d4-led-firmware-test
'
```

実機でのUSB通信・発光・10ms周期の維持は未確認。テープの長さを確定した後、最大接続本数と実際のLED数で出力時間を確認する。
