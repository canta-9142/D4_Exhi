# Web管理API

## 1. 対象と責務

管理クライアントがゲーム状態を取得し、管理操作を要求するHTTP APIと、その利用方法を定義する。ゲームルールは[ルールの正本](../rules.md)、操作の受理条件と状態変更は[サーバー仕様](./server.md)に従う。

`web`はHTTPの形式を検証し、状態取得を`app.State(ctx)`、管理操作を`app.Execute(ctx, command)`へ対応付ける。対応表やゲーム状態を直接変更しない。以下で定義するJSONは管理画面向けの表現とし、MCUとの通信形式とは区別する。

管理画面のレイアウトは本仕様の対象外とする。対応表の保存・復元は[サーバー仕様4.2.3](./server.md#423-デバイス対応表の保存復元)、ゲーム記録の保存とベストスコアの集計は[サーバー仕様3.5](./server.md#35-ゲーム記録とベストスコア)に従い、保存専用のHTTP APIは設けない。

## 2. 共通形式

| メソッド・パス | 用途 |
|---|---|
| `GET /api/state` | 現在状態のSnapshotを取得 |
| `POST /api/commands` | 管理操作を1件要求 |

JSONはUTF-8とし、APIが生成する応答のContent-Typeは`application/json`とする。POSTもContent-Typeを`application/json`とし、本文に1つのJSONオブジェクトを指定する。数値・真偽値・文字列の暗黙の型変換は行わない。文字列の大文字・小文字は区別する。

| 項目 | 公開形式 |
|---|---|
| プレイヤーノードID | `P1`〜`P8` |
| BotノードID | `B1`〜`B3` |
| 機器・割当先の種別 | `player`・`led` |
| LED制御ノードID | `B1`・`B2`・`B3`・`P1`・`P3`・`P5`・`P7` |
| フェーズ | `Preparing`・`Playing`・`Finished` |
| プレイヤーの操作モード | `manual`・`auto` |
| Device ID | MCUの識別で得たUIDの文字列。そのまま照合し、別の表記へ補正しない |

公開ノードIDの`P1`〜`P8`は内部IDの1〜8、`B1`〜`B3`は9〜11に対応する。画面のプレイヤー表示名A〜H、Bot表示名I〜Kとの対応は[プレイヤーノード仕様](./player.md)に従う。例えばプレイヤーP1とLED制御P1は別の割当先なので、割当では種別とノードIDを組み合わせる。

## 3. 状態取得

### 3.1. GET /api/state

本文なしで要求し、成功時は`200 OK`でSnapshotを返す。応答には`Cache-Control: no-store`を付ける。

1回の`app.State`が返した整合したコピーをJSONへ変換する。項目ごとに別の時点の状態を読み直さない。GETは時間進行・生成・転送・準備完了・割当などを変更せず、ゲーム時刻も最後に確定した値を使用する。

以下は`players`・`bots`を一部省略した例である。実際の応答は全プレイヤー・全Botを含む。`records`は、13時台に1件の対象ゲームが終わった後のPreparingを表す。

```json
{
  "game": {
    "phase": "Preparing",
    "durationSeconds": 90,
    "elapsedMs": 0,
    "remainingSeconds": 90,
    "countdownSeconds": 0,
    "canStart": false,
    "startBlockers": [
      {"code": "players_not_ready", "nodeIds": ["P1"]}
    ]
  },
  "players": [
    {
      "nodeId": "P1",
      "mode": "manual",
      "ready": false,
      "fifoLength": 0,
      "score": 0,
      "rankingEligible": null,
      "rank": null
    }
  ],
  "bots": [
    {"nodeId": "B1", "fifoLength": 0, "ratePps": 1}
  ],
  "devices": [
    {
      "deviceId": "001122334455",
      "kind": "led",
      "connected": true,
      "assignment": null
    },
    {
      "deviceId": "AABBCCDDEEFF",
      "kind": "player",
      "connected": true,
      "assignment": {"kind": "player", "nodeId": "P1"}
    }
  ],
  "preview": {"deviceId": "001122334455", "nodeId": "P1"},
  "records": {
    "gameBest": null,
    "hourlyBests": [
      {"hourStart": "2026-10-28T13:00:00+09:00", "score": 812, "nodeIds": ["P3"], "finishedAt": "2026-10-28T13:05:12+09:00"}
    ],
    "allTimeBest": {"score": 812, "nodeIds": ["P3"], "finishedAt": "2026-10-28T13:05:12+09:00"},
    "available": true,
    "unavailableReason": null,
    "saveFailed": false
  }
}
```

### 3.2. Snapshotの項目

トップレベルの`game`・`players`・`bots`・`devices`・`preview`・`records`は毎回含める。対象のない配列は`[]`、未割当の`assignment`と対象のない`preview`は`null`とし、フィールドを省略しない。

| gameの項目 | 型と意味 |
|---|---|
| phase | 現在のフェーズの文字列 |
| durationSeconds | ルールで定めるゲーム時間の整数秒。現行90 |
| elapsedMs | 確定済み経過時間をミリ秒未満切り捨てで表した非負整数。Preparingと、Playingの開始直後のカウントダウン中は0。Finishedは終了時点の値を保持 |
| remainingSeconds | 0〜durationSecondsの整数。MCUの`status.remaining`と同じ計算・保持規則に従う。カウントダウン中はdurationSeconds |
| countdownSeconds | ゲーム時間が始まるまでの秒数を表す0〜3の整数。Startの受理から3秒間のカウントダウン中は3・2・1、それ以外は0。MCUの`status.countdown`と同じ値 |
| canStart | 現在のStart受理条件を満たすかの真偽値 |
| startBlockers | 満たしていないStart受理条件の配列。開始可能なら空配列 |

`players`はP1〜P8の8件、`bots`はB1〜B3の3件を番号順に返す。機器が未接続でも論理ノードを省略しない。配列の区別でプレイヤー・Botを表し、Botは常に自動操作とする。

| playersの各要素 | 型と意味 |
|---|---|
| nodeId | プレイヤーノードID |
| mode | `manual`または`auto` |
| ready | サーバーが受理済みの準備完了状態。autoでは常にfalse |
| fifoLength | 正本FIFOの件数を表す非負整数。確認表示中も正本の件数を返す |
| score | 現在スコアの整数。0〜2147483647 |
| rankingEligible | Preparingでは未確定を表すnull。Playing・Finishedでは当該ゲームのランキング資格を表す真偽値 |
| rank | Finishedのランキング対象者は最終順位を表す1〜8の整数。Preparing・Playing、およびFinishedのランキング対象外はnull |

`rankingEligible`はStartで設定した資格とゲーム中の失効を反映し、Finishedでは確定した結果を保持する。Finishedでのモード・割当変更から再計算しない。Reset後はPreparingの`null`へ戻す。

`rank`は`app`がFinishedへの遷移時に確定した順位をそのまま返す。順位の決め方は[ゲームのルール](../rules.md#最終順位)に従い、同点の対象者は同順位とする。`rankingEligible`と同じく、Finishedでのモード・割当変更から再計算しない。Reset後はPreparingの`null`へ戻す。Playing中は途中の順位を返さず、常にnullとする。Finishedでは、この値はプレイヤーの`summary`の`rank`と同じであり、`rank`が整数であることと`rankingEligible`が`true`であることは同値となる。

| botsの各要素 | 型と意味 |
|---|---|
| nodeId | BotノードID |
| fifoLength | 正本FIFOの件数。0〜4の整数 |
| ratePps | 設定済みの処理速度。有限の正数。初期値1 |

`devices`にはサーバープロセス内で保持している識別済み機器を返す。未割当・切断中の機器も含め、Device ID順で返す。保存ファイルから復元された機器も含め、起動後に接続・識別されるまでは`connected:false`とする。

`players`・`bots`は計11個の論理ノードを表し、`devices`は物理MCUを表す。通常構成のプレイヤー基板8台とLED制御基板7台をすべて識別済みなら、`devices`には計15件を含める。未割当の予備基板や交換後の旧基板も含むため、件数は15件に固定しない。同じ`nodeId`でも`assignment.kind`が異なるプレイヤー基板とLED制御基板は別の機器として返す。

| devicesの各要素 | 型と意味 |
|---|---|
| deviceId | Device IDの文字列 |
| kind | `player`または`led` |
| connected | 当該機器の有効なシリアル接続があるかの真偽値 |
| assignment | 正式な割当の`{"kind":"player","nodeId":"P1"}`形式、またはnull。kindは機器種別と一致する |

`preview`がある場合は対象の`deviceId`と確認用の`nodeId`を含める。機器種別は`devices`から取得する。未割当基板への一時的なP1設定もここへ返し、`devices[].assignment`を変更しない。SnapshotのFIFO・スコア・準備完了状態へ確認用の値を混ぜない。

`records`はベストスコアと記録ファイルの状態を表す。`records`の各項目も省略しない。集計の対象・同点・時間帯の規則は[サーバー仕様3.5.5](./server.md#355-ベストスコアの集計)と[ゲームのルール](../rules.md#ベストスコア)に従う。

| recordsの項目 | 型と意味 |
|---|---|
| gameBest | Finishedのゲームでの、ランキング対象者の最高点を表すベストスコア。手動の終了でも返す。Preparing・Playingと、対象者のいないゲームではnull |
| hourlyBests | 1時間ごとのベストスコアの配列。対象の結果がある時間帯をすべて、新しい時間帯から順に含める。なければ`[]` |
| allTimeBest | 通算のベストスコア。対象の結果がなければnull |
| available | 起動時に記録ファイルを使えたかの真偽値 |
| unavailableReason | availableがfalseのとき、記録ファイルを使えなかった理由の文字列。trueのときはnull |
| saveFailed | 直近のゲーム記録の保存に失敗したかの真偽値。次の保存に成功するとfalseに戻る。起動後にまだ保存していない場合と、availableがfalseの場合はfalse |

| ベストスコアの項目 | 型と意味 |
|---|---|
| hourStart | hourlyBestsの要素だけに含める。時間帯の始まりの正時 |
| score | 最高点の整数。0〜2147483647 |
| nodeIds | その点を出したプレイヤーノードIDの配列。同点の対象者を全員、番号順に含める。1件以上 |
| finishedAt | その点を出したゲームが終わった時刻 |

時刻は日本時間の`+09:00`を付けたRFC3339形式の、秒までの文字列とする（例: `2026-10-28T13:05:12+09:00`）。秒未満は切り捨てる。

`available`がfalseのとき、`hourlyBests`・`allTimeBest`はこの起動中に終わったゲームだけの集計となる。管理画面は、その旨と`unavailableReason`を表示する。`unavailableReason`はログと同じ内容の説明であり、画面の分岐に文面を使用しない。

### 3.3. 開始不可理由

`startBlockers`には該当する条件を次の順で各1件返す。

| code | 意味・追加項目 |
|---|---|
| not_preparing | Preparingではない |
| pid_exhausted | 次ゲームの最大生成数1000件分の未発行PIDがない。Resetで採番を0に戻すため、通常は発生しない |
| players_not_ready | manualの未準備プレイヤーがある。nodeIdsに対象IDを番号順で列挙 |

各要素は`code`を必須とし、`nodeIds`は`players_not_ready`の場合だけ含める。該当する理由はすべて返し、理由が空であることと`canStart=true`を一致させる。manualが0台なら準備完了条件を満たす。

管理画面はmanualノードの準備状況と開始不可理由を表示し、`canStart=true`の場合だけ開始操作を有効にする。Snapshot取得後の状態変更もあるため、Startの要求時には`app`が受理条件を再検証する。

## 4. 管理操作

### 4.1. POST /api/commands

要求のトップレベルは`type`と`data`だけを含む。`type`は次表の文字列、`data`はその操作の引数オブジェクトとし、引数なしでも`{}`を明記する。

```json
{"type":"set_node_mode","data":{"nodeId":"P1","mode":"manual"}}
```

| type | dataの例 | appへの対応 |
|---|---|---|
| start | `{}` | Start |
| finish | `{}` | Finish |
| reset | `{}` | Reset |
| set_node_mode | `{"nodeId":"P1","mode":"manual"}` | SetNodeEnabled |
| set_bot_rate | `{"nodeId":"B1","ratePps":1.5}` | SetBotRate |
| set_device_assignment | `{"deviceId":"AABBCCDDEEFF","target":{"kind":"player","nodeId":"P1"}}` | SetDeviceAssignment |
| swap_device_assignments | `{"firstDeviceId":"AABBCCDDEEFF","secondDeviceId":"112233445566"}` | SwapDeviceAssignments |
| replace_device_assignment | `{"oldDeviceId":"AABBCCDDEEFF","newDeviceId":"112233445566"}` | ReplaceDeviceAssignment |
| set_device_preview | `{"deviceId":"001122334455"}` | SetDevicePreview |

各操作の例は独立したものとし、swap・replaceの2台は同じ機器種別を想定する。表に示した各引数は必須とし、トップレベル・data・targetに未知のフィールドがある場合も拒否する。操作名、列挙値、ノードID、型、必須項目の不正は`400 invalid_request`とする。

`set_node_mode`はプレイヤーだけを対象とし、`manual`をSetNodeEnabledの有効、`auto`を無効へ対応付ける。`set_bot_rate`はBotだけを対象とし、ratePpsは有限の正数とする。0・負数・文字列を受け付けない。

`set_device_assignment`の解除は`target:null`、`set_device_preview`の停止は`deviceId:null`を指定する。nullを許すのはこの2項目だけとし、省略で代用しない。それ以外のDevice IDは空でない文字列とする。swap・replaceでは異なる2台を指定する。同一Device IDの指定は`400 invalid_request`とする。

```json
{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":null}}
{"type":"set_device_preview","data":{"deviceId":null}}
```

`target`のkindとnodeIdの組み合わせは2節の範囲に従う。既知機器の種別との不一致、割当の競合、フェーズ、接続状態、準備完了などの条件は、[サーバー仕様4.2](./server.md#42-管理クライアントからの入力)に従って`app`が処理時点で検証する。複数クライアントからの要求も`app`の受け付け順で処理する。

### 4.2. 成功応答

操作の確定後に`200 OK`で次を返す。

```json
{"ok":true}
```

成功は`app`で管理操作が確定したことを表し、MCUによる受信・適用完了を保証しない。変更後のSnapshotはこの応答に含めず、クライアントが`GET /api/state`で再取得する。操作間に別のイベントが入る可能性があるため、再取得したSnapshotを当該操作直後の状態に限定しない。

同じ設定の再要求や、確認表示の対象がない場合の停止などは、サーバー仕様で受理される限り成功とする。

## 5. 拒否・エラー

APIが生成するエラー応答は次の共通形式とする。状態取得時のエラーにも用いる。

```json
{
  "ok": false,
  "error": {
    "code": "players_not_ready",
    "message": "準備が完了していないプレイヤーがいます。",
    "details": {"nodeIds": ["P1"]}
  }
}
```

`code`・`message`・`details`は必須とする。codeは機械判定用の文字列、messageは表示用の説明とし、画面の分岐にmessageの文面を使用しない。detailsは追加情報のオブジェクトとし、情報がなければ`{}`を返す。

| HTTPステータス | code | 用途 |
|---|---|---|
| 400 | invalid_request | JSON、未知の操作・フィールド、引数の型・範囲・必須項目などの不正 |
| 404 | not_found | 未知のDevice IDまたはエンドポイント |
| 405 | method_not_allowed | 既知のエンドポイントに対する未対応メソッド。Allowヘッダーで対応メソッドを返す |
| 409 | phase_conflict | 現在のフェーズでは受理できない操作 |
| 409 | pid_exhausted | Startに必要な未発行PIDが不足。Resetで採番を0に戻すため、通常は発生しない |
| 409 | players_not_ready | manualの未準備プレイヤーがある。details.nodeIdsに対象IDを番号順で列挙 |
| 409 | assignment_conflict | 通常割当の対象が他UIDに使用されているなど、対応が競合 |
| 409 | device_disconnected | 操作に必要な機器が接続・識別済みでない |
| 409 | device_kind_mismatch | 機器と割当先の種別、または交換する機器同士の種別が不一致 |
| 409 | device_not_assigned | swap・replaceの交換元など、割当済みである必要がある機器が未割当 |
| 409 | manual_mode_conflict | manualのプレイヤーに対する確認表示の開始 |
| 415 | unsupported_media_type | POSTのContent-Typeがapplication/jsonでない |
| 500 | internal_error | デバイス対応表の保存失敗、その他の内部エラー |
| 503 | unavailable | アプリ停止中など、要求を受け付けられない状態。管理操作では6節に従い結果不明として扱う |

Startで複数の条件を満たさない場合の409は、3.3の順で最初に該当する理由を返す。`not_preparing`はHTTPエラーの`phase_conflict`へ対応付け、残りの2種類は同じcodeを使用する。その他の操作で複数の拒否理由がある場合は、該当する理由の一つを返す。

形式が不正な要求や、未知のDevice ID、操作の受理条件を満たさない要求では、要求された管理操作を適用しない。割当・解除・交換・付け替えでは、保存成功後に変更を確定して成功応答を返す。置換前の保存失敗・置換自体の失敗では`500 internal_error`を返し、その操作に伴う対応表・自動仮割当の対象外情報・操作モード・準備完了状態・確認表示を変更しない。ゲームの通常の時間進行はサーバー仕様に従って継続する。置換後の親ディレクトリ同期失敗では保存結果を不明とし、成功応答を返さずサーバーを停止する。クライアントは内部エラー・切断・タイムアウト時の結果不明として扱い、6節に従って自動再送せず、再起動後の状態を取得する。存在していて切断中のDevice IDは404にせず、対象操作が接続を必要とする場合に409で拒否する。

## 6. 管理画面からの取得と再試行

| 契機 | クライアントの動作 |
|---|---|
| 初回表示・タブの再表示 | 直ちにGETで状態取得 |
| 通常表示中 | 状態取得完了から500ms後に次のGETを送信 |
| 管理操作の応答後 | 成功・拒否のどちらでも直ちにGETで再取得 |
| 状態取得の失敗 | 通信途絶として表示し、1秒間隔でGETを再試行 |
| タブが非表示 | 定期取得を停止 |

状態取得は常に同時1件とし、操作後の再取得と定期取得を直列化する。古い取得応答が操作後の再取得結果を上書きしないようにする。同じ画面内の管理操作も同時1件とする。POST送信時に追加の管理操作を無効にし、応答・通信失敗・タイムアウトのいずれかが確定してからGETで再取得する。その取得が成功して画面の状態更新が完了するまでを処理中とし、POSTの応答だけでは操作を再開しない。再取得に失敗した場合は無効を維持し、再試行による取得・画面更新が成功するまで待つ。操作前から実行中だったGETの完了では、この無効状態を解除しない。

HTTP要求のクライアント側タイムアウトは5秒とする。初回の状態取得が成功するまでは管理操作を無効にする。状態取得に失敗した場合は前回値を保持したまま通信途絶を明示し、次の取得成功まで管理操作を無効にする。取得したスコア・残り時間を表示し、画面側で独自の加点やカウントダウンを行わない。

管理操作は通信切断・タイムアウト・HTTPエラーのいずれでも自動再送しない。切断・タイムアウト・内部エラー・`503 unavailable`では操作結果が不明な場合があり、既に`app`へ到達した操作が取り消されたとはみなさない。`503 unavailable`は、`app`が操作を受け取ったあとに停止した場合にも返るため、操作が適用されなかったことを表さない。結果不明を表示して状態を再取得し、必要な再操作は管理者が判断する。特にswapの再送は割当を元に戻し、resetの再送は再度初期化するため、GETと同じ再試行処理を使用しない。
