# D4 学科展示 — パケット配送ゲーム(仮)

TCP/IP のパケット配送から着想を得た展示用ゲーム。外周 8 台のプレイヤーノードと内側 3 台のボットノードを LED ストリップの「ルート」で結び、プレイヤーは自ノードに届いたパケットをボタンで隣接ノードへ振り分ける。振り分けたパケット数などでスコアが決まる。

- 概要・予算: [docs/summary.md](docs/summary.md)
- ルール(ノード ID・ボタン配置): [docs/rules.md](docs/rules.md)
- 通信仕様(サーバー↔各ノード): [docs/spec.md](docs/spec.md)
- プレイヤーノード実装詳細: [docs/details/player.md](docs/details/player.md)
- ブラウザ上のシミュレーター: [demo/network_game_sim.html](demo/network_game_sim.html)

## ディレクトリ構成

```text
.
├── demo/           ゲームシミュレーター(単体 HTML)
├── docs/           概要・ルール・通信仕様・実装詳細
├── server/         ゲームサーバー(Go)
├── src/
│   ├── player/     プレイヤーノードのファームウェア
│   └── led/        LED ノードのファームウェア
├── tests/          ホストテスト・実機通信テスト
└── flake.nix       開発環境
```

## 開発環境

Nix を使う場合は、flake に必要なツールがそろっている。

```sh
nix develop        # もしくは direnv allow
```

Nix を使わない場合は、次のツールを個別にインストールする。

- [arduino-cli](https://arduino.github.io/arduino-cli/latest/installation/)(ESP32 ボードパッケージ `esp32:esp32`)
- Go(`server/go.mod` に記載のバージョン以上)
- 任意: `gopls`、`golangci-lint`

```sh
arduino-cli core update-index
arduino-cli core install esp32:esp32
```

プレイヤーノードのビルド・テスト手順は [docs/details/player.md](docs/details/player.md#ビルド) を参照。Nix を使わない場合は、手順中の `nix develop -c` / `nix shell ... -c` を外して実行する。

```sh
arduino-cli lib install ArduinoJson@7.4.3
arduino-cli compile \
  --fqbn esp32:esp32:esp32s3:USBMode=hwcdc,CDCOnBoot=cdc,DebugLevel=none,PSRAM=enabled \
  --build-path /tmp/d4-player-cdc-build src/player
```
