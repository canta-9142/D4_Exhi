# 2026年度 神戸高専 電子工学科／システム情報工学科 学科展示 — パケット配送ゲーム

https://github.com/user-attachments/assets/f73442ef-061d-4928-86bf-a45ec40dbc0b

TCP/IP のパケット配送から着想を得た展示用ゲーム。外周 8 台のプレイヤーノードと内側 3 台のボットノードを LED ストリップの「ルート」で結び、プレイヤーは自ノードに届いたパケットをボタンで隣接ノードへ振り分ける。振り分けたパケット数などでスコアが決まる。

- 概要・予算: [docs/summary.md](docs/summary.md)
- ルール(ノード ID・ボタン配置): [docs/rules.md](docs/rules.md)
- 通信仕様(サーバー↔各ノード): [docs/spec.md](docs/spec.md)
- プレイヤーノード実装詳細: [docs/details/player.md](docs/details/player.md)
- ブラウザ上のシミュレーター: [demo/network_game_sim.html](demo/network_game_sim.html)

## ディレクトリ構成

```text
.
├── 3d/             筐体の3Dモデル
├── demo/           ゲームシミュレーター(単体 HTML)
├── docs/           概要・ルール・通信仕様・実装詳細
├── server/         ゲームサーバー(Go)
├── src/
│   ├── player/     プレイヤーノードのファームウェア
│   └── led/        LED ノードのファームウェア
├── tests/          ホストテスト・実機通信テスト
└── flake.nix       開発環境
```
