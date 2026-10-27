# サーバー実装

仕様の正本は[サーバー仕様](../spec/server.md)、[ゲームルール](../rules.md)、
[プレイヤー通信仕様](../spec/player.md)とする。

## 起動と検証

プロジェクトルートで実行する。

```sh
nix develop -c go -C server run ./cmd/server
nix develop -c go -C server run ./cmd/server -log-level debug
nix develop -c go -C server run ./cmd/server -http-addr 127.0.0.1:8080
nix develop -c go -C server run ./cmd/server -list-serial-ports
nix develop -c go -C server run ./cmd/server -serial-port /dev/ttyACM0 -serial-port /dev/ttyACM1
nix develop -c go -C server test -race ./...
nix develop -c go -C server vet ./...
```

`main`はログ設定を読み込み、標準出力へのログ、Transport、`app`を初期化する。
指定ポートごとにUSB CDC接続を管理するgoroutineを起動する。
HTTPは`-http-addr`（既定`:8080`）で待ち受け、管理APIと並行してserialを処理する。
SIGINT・SIGTERMではHTTPを最大5秒待って停止し、ポートの再接続処理を停止する。
有効な接続のI/O終了と`Disconnected`の通知を待ってから`app`を終了する。

USB CDCの接続先ホストはLinuxを前提とする。`-list-serial-ports`は
`/dev/ttyACM*`・`/dev/ttyUSB*`の候補を昇順で表示して終了し、ポートは開かない。
候補なしの場合は何も表示しない。VID/PIDによる機器種別判定や自動接続は行わず、
接続対象を繰り返し指定可能な`-serial-port`で選ぶ。同じパスの重複指定と空の指定は拒否する。
ポート未指定でも起動でき、その場合はMCU未接続のPreparingで待機する。

指定したポートが起動時に存在しない場合や、接続終了・オープン失敗時は1秒後に再試行する。
USBの抜き差しで`ttyACM`等の番号が変わる環境では、`/dev/serial/by-id/...`などの
安定したパスも指定できる。開いたポートのパスとSession IDはログへ記録する。
受信形式と有効なHELLOからプレイヤー／LEDを識別し、再接続でもゲームや割当の正本を維持する。

`web`の`/api/state`・`/api/commands`は`app`へ接続済み。管理画面は仮の表示である。

### Linuxのポート設定と検証範囲

`serial.OpenPort`はTTYを115200bps・8N1・フロー制御なしのrawモードへ設定する。
制御線を持つTTYではDTRを有効、RTSを無効にする。排他オープンとファイルロックで、
同じTTYの別名を含む二重接続を防ぐ。再オープンのため、終了時に排他指定を解除する。
非ブロッキングのファイル記述子を`os.File`へ渡し、GoのpollerにRead・Writeの待機を任せる。
Closeで両方の待機を解除できることを確認したポートだけを使用する。
OS設定には`golang.org/x/sys/unix`を使用する。

Linuxの疑似TTYでraw設定、改行を変換しない送受信、別名での二重接続拒否、
再オープン、Read・Writeの待機解除を検証する。`main`からのプレイヤー・LED同時接続と
終了処理、ポート不在時の再試行と切断後の再接続も自動テストの対象とする。
実機のUSB抜き差し、DTR/RTSによる基板の挙動、およびRadxa上での動作は未確認。

## appの呼び出し

`app.New(app.Options{...})`で生成し、`Run(ctx)`を別goroutineで実行する。
同じインスタンスの`Run`は一度だけ呼べる。

| 呼び出し | 用途 |
|---|---|
| `Execute(ctx, Command)` | 管理操作。拒否理由はerrorで返す |
| `State(ctx)` | ゲームとデバイスの独立したSnapshotを取得 |
| `Connected(ctx, SessionID)` | 新規シリアルセッションの登録と識別開始 |
| `Disconnected(ctx, SessionID)` | 切断通知。旧セッションの通知は無視 |
| `MessageReceived(ctx, SessionID, IncomingMessage)` | 検証済みのMCUメッセージを処理 |

`Execute`は従来の操作に加え、`SwapDeviceAssignments`、`ReplaceDeviceAssignment`、
`SetDevicePreview`を受け付ける。割当先は`Command.Kind`と`Command.Node`で指定し、
プレイヤーP1とLED制御P1を区別する。交換操作は`DeviceID`と`OtherDeviceID`を使い、
予備基板への交換では前者が交換元、後者が交換先となる。
`SetDeviceAssignment`のNode=0は解除、`SetDevicePreview`の空DeviceIDは確認終了を表す。

状態変更は`Run`のgoroutineだけで行う。要求はチャネルで受け付け、処理開始時の時刻を使う。
`State`は時間を進めない。その他の要求と100ms周期のTickは、未処理の生成・転送を
期限順に処理してから要求を適用する。ゲーム終了時刻ちょうどの生成・転送は実行しない。

呼び出し元はcontextで待機を中断できる。受理後のキャンセルは、操作の取り消しを保証しない。
終了済みの`app`への要求は`ErrStopped`を返す。
Snapshotの経過時間・残り時間は`time.Duration`であり、`RemainingSeconds`は
MCUの`status.remaining`と同じ切り上げ済み残り秒数を返す。HTTPでの時間表現への変換は`web`が行う。
Snapshotにはノードごとの`Ready`・確定順位`Rank`、`CanStart`・`StartBlockers`、機器種別付きの対応表、
現在確認表示中の機器と確認用ノードIDも含む。開始条件の判定はStartとStateで共通化する。

管理操作の拒否は`*app.Rejection`で返し、`errors.As`で`Code`・`Nodes`を取得できる。
開始不可理由は`StartBlocker`の配列で、`not_preparing`、`pid_exhausted`、
`players_not_ready`の順に列挙する。未準備のNodesは番号順でまとめる。
Startの拒否では先頭の理由を使用し、`not_preparing`は`phase_conflict`へ対応付ける。
無効な引数はmainと同じ`CodeInvalid`（`invalid`）で返し、Web側で`invalid_request`へ変換する。
終了済みの`app`は`errors.Is(err, app.ErrStopped)`で判定し、Web側で`503 unavailable`へ対応付ける。

`web`はこの内部Snapshotを[Web管理API](../spec/web.md)の公開JSONへ変換する。
ノードIDの文字列表記、時間単位、Preparingの`rankingEligible:null`、エラーのHTTPステータスと
`details.nodeIds`への変換は`web`が担当する。

## serialとの接続口

`Options.Send`に、`SessionID`と`Message`を送信キューへ登録する関数を渡す。
`Message.MarshalLine()`で通信行へ変換し、LF終端は`serial`が付与する。
プレイヤー向けと共通の`identify`はJSON、LEDの`CONFIG`・`LIGHT`・`BUFFER`・`CLEAR`は
スペース区切りとなる。LEDのノードIDは通信時に`P1`・`B1`等へ変換する。
プレイヤーの`config.preview`とLEDの`CONFIG NODE PREVIEW`は通常false、確認中だけtrueを明記する。
`Options.Close`には物理セッションを閉じる関数を渡す。

### セッション管理と送信キュー

`serial.New(logger)`でTransportを生成し、`Send`・`Close`を`app`のコールバックへ渡す。
開いた通信路ごとに`Serve(ctx, application, port)`を別goroutineで実行する。
Linuxの実ポートには`RunPort(ctx, application, name)`を使うと、オープンと再試行も管理する。

```go
transport := serial.New(logger)
application := app.New(app.Options{
    Logger: logger,
    Send:   transport.Send,
    Close:  transport.Close,
})
go application.Run(ctx)
err := transport.Serve(ctx, application, port)
```

`port`は`io.ReadWriteCloser`を満たし、ReadとWriteを並行実行でき、Closeによって
両方の待機を解除できるものを渡す。`Serve`は通信路の所有権を引き受け、終了時に閉じる。
Session IDはTransportの再生成や再接続でも再利用しないプロセス共通の連番とする。

`Send`は`MarshalLine()`の結果にLFを付け、接続ごとに最大64行のFIFOへ登録する。
書込中の1行はこの件数に含めない。キューには確定したバイト列を格納し、後から
元のMessageのデータを変更しても送信内容を変えない。成功は登録完了だけを表す。
行長超過・エンコード失敗・キュー溢れではerrorを返してセッションを無効化する。
書込失敗・短い書込・受信の異常でも終了し、未送信のキューは再接続へ引き継がない。

`Close`は冪等で、セッションを直ちに無効化する。Send・CloseはI/Oや`app`への通知を待たず、
物理ポートのCloseと待機中のRead・Writeの終了は別goroutineで行う。
受信したバイト列は時刻付きでReceiverへ渡し、受信順に`MessageReceived`を呼ぶ。
`Connected`の処理が完了してからReadと受信メッセージの通知を開始し、
登録待ちの時間を受信停止として扱わない。

受信の組み立て・検証・部分行のタイムアウト監視は、`app`への通知を待たずに進める。
検証済みの入力を接続ごとに最大64件のFIFOへ登録し、別goroutineから受信順に通知する。
`app`が処理中の1件はキュー件数に含めない。`app`の処理待ちを受信停止と判定せず、
処理待ちの間でも実際に部分行の受信が1秒途絶えればセッションを終了する。
入力キューが溢れた場合は`ErrReceiveQueueFull`で終了し、無通信タイムアウトとは区別する。
終了時は未通知の入力を破棄し、進行中の通知処理が戻ってから`Disconnected`を通知する。

`Serve`はポートのCloseとI/O処理の終了後に`Disconnected`を一度通知してから戻り、
最初の終了理由をerrorで返す。接続単位のcontextをキャンセルした場合も、
まだ動作中の`app`への切断通知は取り消さない。終了したSession IDへの追加送信は拒否する。
`app.Run`はServe開始前に起動し、Serve終了まで動作させるか、終了処理を完了させる。

### コールバックの制約

コールバックから`app`のメソッドを同期的に呼び出してはならない。
`Send`はMCUの受信やシリアル書込の完了を待たずに戻る必要がある。
送信登録の失敗時は`app`も当該セッションを無効化し、プレイヤーを自動操作へ切り替える。
後から届く`Disconnected`は無害に処理する。確定したゲーム処理は取り消さない。
過去の期限を処理している途中で失敗した場合、送信と物理セッションは直ちに停止するが、
ゲーム上の自動操作への切替は処理開始時刻まで追い付いてから適用する。
未処理区間の手動操作を遡って自動操作に変えず、次の自動転送期限も切替時刻から計る。

`serial`はJSON・必須フィールド・型・範囲・行長などを検証してから`IncomingMessage`へ変換する。
識別前はプレイヤーのJSON `hello`とLEDの`HELLO UID`の両形式を判定し、
どちらも`Type="hello"`として、`Kind=PlayerDevice`または`Kind=LEDDevice`を設定する。
両種別のUIDを大文字16進12桁として検証する。
LEDのHELLO候補はASCII・96バイト上限・スペース／タブの区切りを検証し、
LED識別後はその接続の行長上限を96バイトに保つ。
`IncomingMessage.Identification`は読取開始時の識別世代とし、
`app`は現在の識別世代と異なる入力を適用時にも無視する。
セッションIDは再接続を含めてプロセス内で再利用しない。
識別前の旧入力破棄、HELLO再受信時の要求ID履歴維持、同期世代の更新とSENDの重複排除は
`app`が担当する。同期世代には暗号学的乱数による128bitの識別子を使う。

### serialの受信処理

`serial.Receiver`をセッションごとに生成する。ゼロ値はHELLO待ちで、
`Feed(data, now, handle)`に受信バイト列と受信時刻を渡す。
LF・CRLFで行を区切り、検証した`IncomingMessage`を受信順に`handle`へ渡す。
未定義の追加JSONフィールドは無視し、必須フィールドの欠落・null・型変換は許容しない。
ゲーム上の受理条件や要求IDの重複排除は行わない。

HELLO待ちでは不正な行とHELLO以外を無視する。プロトコルの上限を超えた行や、
受信が1秒途絶えた部分行はLFまで破棄する。有効なHELLOから機器種別を記録し、
以後の不正入力・別種別の通信形式はerrorを返す。
無通信中も`CheckTimeout(now)`を呼ぶ。期限があるのは行途中だけで、行間の無通信は許容する。

`app`は新たにHELLO待ちへ移る`identify`だけ、送信要求の`BeginIdentification`をtrueにし、
セッション内の識別世代`Identification`を1から昇順に発行する。待機中の再送では
フラグをfalseにして同じ世代を使う。世代が上限に達したセッションは終了し、周回させない。
フラグと識別世代は内部用で、MCUへ送るJSONには含めない。
`serial`はフラグがtrueの送信要求をキューへ登録する際だけ、追加の入力を処理する前に
`WaitHello(message.Identification)`を呼ぶ。これにより受信途中の旧行をLFまで破棄する。
再送では受信中のHELLOや既存の破棄状態を変更しない。同じ世代や過去の世代を渡した場合も
Receiverの状態は巻き戻さない。
Read開始前に識別世代を固定し、解析した入力にもその世代を引き継ぐ。
世代の切り替えをまたぐReadの結果は旧世代として破棄する。そのReadに新しい応答が
含まれていた場合も、同じ世代を使う1秒周期の`identify`再送で再取得する。
再識別前に受信済みの古いバイト列は、改行の境界を維持しながら破棄する。
新しい識別世代を、配信待ちの古いHELLOへ付け直さない。
この世代はFIFO同期の`sync`とは独立し、ゲームResetや通常の再同期では更新しない。
受信側での解析と`app`でのHELLO受理の間に再送されても、識別状態を巻き戻さない。
`Feed`と`CheckTimeout`は受信側の同じgoroutineから直列に呼ぶ。
`WaitHello`は別goroutineの`app.Options.Send`からも呼べる。要求された世代を記録して直ちに戻り、
状態変更は受信側が次の入力・タイムアウト確認の前に適用する。
`handle`を呼ぶ間にReceiverのロックを保持する必要はない。

識別前の不正な入力・行長超過・部分行のタイムアウトは、破棄時に`Logger`へ記録する。
受信開始前にセッションID付きの`slog.Logger`を設定でき、未設定時は標準出力へ記録する。
LFまでの破棄中に同じ原因を繰り返し記録せず、正常な旧SEND等の無視は異常として記録しない。
ReceiverはシリアルI/Oや`app`への通知を行わない。`Feed`・`CheckTimeout`または`handle`のerrorは
終端状態として保持し、物理切断・`Disconnected`の通知はTransportで行う。
再接続時は新しいReceiverを使い、旧接続の部分行を引き継がない。

## 準備完了と表示通知

Preparingのmanualプレイヤーから現在の同期世代のREADYを受けた場合だけ、準備完了を確定する。
割当済みの有効なセッションには受理・拒否・重複のいずれでもSTATUSを返す。
READYはSENDの要求IDを消費しない。全manualプレイヤーの準備完了後も管理者のStartを待ち、
manualが0台なら全自動で開始できる。Playing中の管理操作によるモード変更は拒否する。

STATUSにはスコア・切り上げた残り秒数・準備完了状態を毎回含める。
初期同期・再同期・フェーズ変更・READY応答では必ず送り、通常は表示値が変化した場合だけ送る。
入力イベントの時間進行と操作を確定してから送信するため、遅延Tickの途中経過を蓄積しない。
ゲームResetでは`phase` → 空FIFOの`reset` → 初期値の`status`の順になる。
確認表示からの復旧では、`config(preview=false)`から現在の`phase`・`status`までを送信する。
Startや別機器への確認切替は、この復旧通知の登録後に行う。

Finishedへの遷移時に、対象者だけのスコア比較で順位を確定する。同点は同順位とし、
100・100・80なら1・1・3となる。最終スコア・資格・順位はモード変更や基板交換でも保持する。
通常表示の割当済みMCUには`phase(Finished)` → `status` → `summary`を送る。
Finished中の再同期・再設定でも、現在の同期世代の`summary`をSTATUSの後に送る。
拒否したSENDなどによるRESETも、`reset` → `phase` → `status` → `summary`で結果を再取得させる。
対象外はeligible=false・rank=null、確認中や未割当にはSUMMARYを送らない。

## デバイス運用と実機確認

機器の初回識別時はPreparing・Finishedに限り、機器種別ごとの空きIDへ昇順で仮割当する。
空き待ちの機器はUID順で再試行する。未接続の機器の割当も予約として保持する。
管理操作で解除されたUIDは自動仮割当の対象から外し、Reset・再接続でもこの扱いを維持する。
交換時は対応表を一括確定してから通信し、ノードのFIFO・得点・ランキング結果は移動しない。
未割当になる機器には両種別とも`identify`を送り、旧設定と確認表示を解除する。

確認表示は全体で1台とし、正本状態とは独立して管理する。プレイヤーには
`config(mode=auto, preview=true)`で設定し、FIFOは割当済みなら正本、未割当なら空配列を送る。
ダミーパケットは生成・送信しない。未割当機器にはP1を一時設定するが、
正式な対応表を変更せず、ゲーム操作や通常通知の対応先にも使用しない。
LEDには`CONFIG NODE true`だけを送り、点滅はMCU内で行う。
Start・Reset・割当／モード変更・HELLO／RESYNC・切断に合わせ、確認を終了・復旧する。

確認終了時は通常の設定から再同期し、FinishedのプレイヤーにはSUMMARYまで復旧する。
未割当の確認対象には`identify`を送る。確認中のプレイヤーをResetする場合は、
`config(preview=false)`を先に送り、その後は通常の`phase(Preparing)`・空FIFO・STATUSで復旧する。

LEDの通常設定は`CONFIG NODE false` → Bot用なら現在件数の`BUFFER`とする。
ゲームResetでは、確認中の割当済みLEDだけに`CONFIG NODE false` → Bot用なら`BUFFER 0`を送る。
通常表示のLEDには`CLEAR` → Bot用なら`BUFFER 0`を送り、CONFIGを再送しない。
確認中の未割当LEDには`identify`を送り、復旧命令を重複させない。
パケット移動は担当リンク表に対応する`LIGHT`へ、BotのFIFO使用量変更は`BUFFER`へ変換する。

## ゲーム処理

`game.go`がゲーム状態と期限処理、`app.go`が管理操作・セッション・プレイヤーへの通知を扱う。
外部ライブラリは使用しない。

自動転送の優先順位は、起動時に宛先ごとの最短ホップ数をBFSで求めて固定する。
転送先から宛先までのホップ数が少ない順、同距離ではノードIDの昇順とする。
混雑や通過済みノードによる優先順位の変更は行わない。
接続関係はプレイヤー通信仕様の表と一致させ、Bot側はその逆向きの接続を持つ。

`devices.go`は機器種別ごとの割当と交換、`output.go`はプレイヤー／LEDへの通知、
`preview.go`は確認表示と復旧を担当する。
ゲームの転送処理は状態を確定し、通知する出来事を戻り値で返す。
手動SENDではRESULTを先に送信し、その後で返された通知を処理する。
実際のUSB接続・受信パーサー・HTTP API、およびスコア・対応表の外部保存は未実装とする。
