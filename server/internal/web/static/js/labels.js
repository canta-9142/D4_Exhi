// Names and wording of the admin page (docs/spec/admin-ui.md 7 and 11); the
// node colours are in style.css. Pure data and functions: nothing here
// touches the DOM. Wording
// is chosen from codes and enum values only; API messages are never parsed
// or shown. A value unknown to the page is shown as it is (admin-ui.md 7.4).

export const PLAYER_IDS = ["P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"];
export const BOT_IDS = ["B1", "B2", "B3"];

// TARGETS are the assignment targets of each device kind, in the order the
// selects list them (admin-ui.md 5.7, web.md 2).
export const TARGETS = {
  player: PLAYER_IDS,
  led: ["B1", "B2", "B3", "P1", "P3", "P5", "P7"],
};

// BOT_FIFO_MAX is the capacity of a Bot FIFO (web.md 3.2).
export const BOT_FIFO_MAX = 4;

// Display names of docs/spec/player.md.
const displayNames = {
  P1: "A", P2: "B", P3: "C", P4: "D", P5: "E", P6: "F", P7: "G", P8: "H",
  B1: "I", B2: "J", B3: "K",
};

const phaseLabels = { Preparing: "準備中", Playing: "ゲーム中", Finished: "終了" };
const operationLabels = { stopped: "停止中", running: "運転中" };
const modeLabels = { manual: "手動", auto: "自動" };
const kindLabels = { player: "プレイヤー基板", led: "LED制御基板" };

export const PHASES = Object.keys(phaseLabels);
export const OPERATIONS = Object.keys(operationLabels);

// The stations in which a round, the mode selection or a result can be cut
// short (admin-ui.md 8.3).
const ACTIVE_STATIONS = ["select", "playing", "finished"];
const RESTING_STATIONS = ["stopped", "idle"];

// isSimultaneous tells which screen to show: the simultaneous game's (admin-ui.md
// 15), or endless operation's for every other mode, including an unknown one.
export function isSimultaneous(game) {
  return game.mode === "simultaneous";
}

export function displayName(nodeId) {
  return displayNames[nodeId] ?? "?";
}

// nodeLabel is "C（P3）" for a player or a Bot.
export function nodeLabel(nodeId) {
  return `${displayName(nodeId)}（${nodeId}）`;
}

// targetLabel is "C（P3）" for a player target and "LED P1" for an LED one.
export function targetLabel(target) {
  return target.kind === "led" ? `LED ${target.nodeId}` : nodeLabel(target.nodeId);
}

export function phaseLabel(phase) {
  return phaseLabels[phase] ?? phase;
}

export function operationLabel(operation) {
  return operationLabels[operation] ?? operation;
}

// canReturnToBot says whether "戻す…" of a player row can be pressed
// (admin-ui.md 8.3): only a station with something to cut short, while
// endless operation runs. An operation or a station this page does not know
// is left to app.
export function canReturnToBot(operation, station) {
  if (RESTING_STATIONS.includes(station)) {
    return false;
  }
  return !OPERATIONS.includes(operation) || operation === "running";
}

// formatElapsed is "1:12:34" (hours, minutes, seconds) for elapsedMs; the
// milliseconds are dropped, not rounded (admin-ui.md 7.4).
export function formatElapsed(elapsedMs) {
  const total = Math.max(Math.floor(elapsedMs / 1000), 0);
  const pad = (n) => String(n).padStart(2, "0");
  return `${Math.floor(total / 3600)}:${pad(Math.floor(total / 60) % 60)}:${pad(total % 60)}`;
}

// roundCounts counts the players in a round by its kind, the countdown
// before a normal round included (admin-ui.md 5.2).
export function roundCounts(players) {
  const playing = players.filter((p) => p.station === "playing");
  return { normal: playing.filter((p) => p.round === "normal").length, tutorial: playing.filter((p) => p.round === "tutorial").length };
}

export function roundCountsText({ normal, tutorial }) {
  return `通常 ${normal} / 練習 ${tutorial}`;
}

// stationText is the "状態" cell of a player row (admin-ui.md 5.3): its text
// and whether it is shown in the caution tone. The seconds are the polled
// values as they are. A value this page does not know is shown as it is.
export function stationText(player) {
  const { station, round, countdownSeconds, remainingSeconds, windowSeconds } = player;
  switch (station) {
    case "stopped":
      return { text: "停止中", tone: "muted" };
    case "idle":
      return { text: "待機", tone: "" };
    case "select":
      return { text: `選択中 残り ${windowSeconds} 秒`, tone: "" };
    case "finished":
      return { text: `結果 残り ${windowSeconds} 秒`, tone: "" };
    case "playing":
      if (round === "normal") {
        return countdownSeconds >= 1
          ? { text: `通常 開始まで ${countdownSeconds} 秒`, tone: "warn" }
          : { text: `通常 残り ${remainingSeconds} 秒`, tone: "" };
      }
      if (round === "tutorial") {
        if (countdownSeconds >= 1) {
          return { text: `練習 開始まで ${countdownSeconds} 秒`, tone: "warn" };
        }
        return windowSeconds <= 10
          ? { text: `練習 無操作で待機まで ${windowSeconds} 秒`, tone: "warn" }
          : { text: "練習", tone: "" };
      }
      return { text: round === null ? station : `${station} ${round}`, tone: "" };
    default:
      return { text: station, tone: "" };
  }
}

export function modeLabel(mode) {
  return modeLabels[mode] ?? mode;
}

export function kindLabel(kind) {
  return kindLabels[kind] ?? kind;
}

// ---------- 開始できない理由（admin-ui.md 11.2） ----------

const blockerTexts = {
  not_stopped: "すでに運転中です",
  not_preparing: "準備中のフェーズではありません",
  pid_exhausted: "次のゲームに必要なパケットIDが足りません。サーバーを再起動してください",
  players_not_ready: "準備完了していない手動プレイヤー",
};

export function blockerText(code) {
  return blockerTexts[code] ?? `開始できない理由（${code}）`;
}

export const NO_MANUAL_HINT = "手動のプレイヤーがいないため、全員自動で始まります";
// The endless operation's notes under the readiness (admin-ui.md 5.2).
export const NO_MANUAL_RUNNING_HINT = "手動のプレイヤーがいないため、全員ボットのまま運転します";
export const RUNNING_HINT = "運転を止めるには、運転停止を押します";
export const FINISHED_HINT = "次のゲームは、リセットして準備中に戻してから始めます";

// The heading over the seconds of the command bar (admin-ui.md 5.2, 7.4).
export const REMAINING_LABEL = "残り時間";
export const COUNTDOWN_LABEL = "開始まで（カウントダウン中）";

// ---------- 状態帯（admin-ui.md 5.1、9） ----------

export const connectionLabels = { connecting: "● 接続中", online: "● 正常", offline: "× 通信途絶" };

export function fetchedText(connection, fetchedAt) {
  if (connection !== "offline") {
    return fetchedAt ? `最終取得 ${fetchedAt}` : "";
  }
  return fetchedAt ? `${fetchedAt} の値を表示中。再接続しています` : "まだ取得できていません。再接続しています";
}

// ---------- 操作の結果（admin-ui.md 10、11.1） ----------

// commandPhrases returns the wording of command for its result: name for
// pending and unknown results, success, and failure as the start of a
// rejection. success is a string, or a function of the response body when
// the wording depends on it. snapshot is the one the command was made from;
// the wording of the screen it was sent from depends on its mode.
export function commandPhrases(command, snapshot = null) {
  const d = command.data ?? {};
  const devices = snapshot?.devices ?? [];
  const simultaneous = snapshot !== null && isSimultaneous(snapshot.game);
  const make = (name, success, failure) => ({ name, success, failure, simultaneous });
  switch (command.type) {
    case "start":
      if (!simultaneous) {
        return make("運転の開始", "運転を開始しました", "運転を開始できませんでした");
      }
      return make("ゲームの開始", "ゲームを開始しました", "ゲームを開始できませんでした");
    case "stop":
      return make("運転の停止", "運転を停止しました。全員が停止中に戻りました", "運転を停止できませんでした");
    case "return_to_bot": {
      const node = nodeLabel(d.nodeId);
      return make(`${node}のボットへの復帰`, `${node}をボットに戻しました`, `${node}をボットに戻せませんでした`);
    }
    case "finish":
      return make("ゲームの終了", "ゲームを終了しました", "ゲームを終了できませんでした");
    case "reset":
      return make("リセット", "リセットしました。準備中に戻りました", "リセットできませんでした");
    case "set_node_mode": {
      const node = nodeLabel(d.nodeId), mode = modeLabel(d.mode);
      return make(`${node}の${mode}への切り替え`, `${node}を${mode}にしました`, `${node}を${mode}にできませんでした`);
    }
    case "set_all_node_modes":
      if (d.mode === "manual") {
        return make("全員の手動への切り替え", (body) => allManualText(body.skipped), "全員を手動にできませんでした");
      }
      return make("全員の自動への切り替え", "全員を自動にしました", "全員を自動にできませんでした");
    case "set_bot_rate": {
      const node = nodeLabel(d.nodeId);
      return make(`${node}の処理速度の変更`, `${node}の処理速度を ${d.ratePps} pps にしました`,
        `${node}の処理速度を変更できませんでした`);
    }
    case "set_device_preview":
      if (d.deviceId === null) {
        return make("確認表示の停止", "確認表示を止めました", "確認表示を止められませんでした");
      }
      return make(`${d.deviceId}の確認表示`, `${d.deviceId}の確認表示を始めました`,
        `${d.deviceId}の確認表示を始められませんでした`);
    case "set_device_assignment": {
      if (d.target === null) {
        return make(`${d.deviceId}の割当を外す操作`, `${d.deviceId}の割当を外しました`,
          `${d.deviceId}の割当を外せませんでした`);
      }
      const target = targetLabel(d.target);
      return make(`${d.deviceId}の割当`, `${d.deviceId}を${target}に割り当てました`,
        `${d.deviceId}を${target}に割り当てられませんでした`);
    }
    case "swap_device_assignments": {
      const pair = `${d.firstDeviceId}と${d.secondDeviceId}`;
      return make(`${pair}の入れ替え`, `${pair}の割当を入れ替えました`, `${pair}を入れ替えられませんでした`);
    }
    case "replace_device_assignment": {
      const old = devices.find((device) => device.deviceId === d.oldDeviceId);
      const target = old?.assignment ? targetLabel(old.assignment) : "その割当先";
      return make(`${d.oldDeviceId}と${d.newDeviceId}の交代`,
        `${d.newDeviceId}が${d.oldDeviceId}と交代し、${target}になりました`,
        `${d.oldDeviceId}と${d.newDeviceId}を交代できませんでした`);
    }
    default:
      return make(`「${command.type}」`, `「${command.type}」を実行しました`, `「${command.type}」を実行できませんでした`);
  }
}

export function pendingText(phrases) {
  return `${phrases.name}を送信しています…`;
}

// successText is the success of a POST. body is the response body.
export function successText(phrases, body) {
  return `✓ ${typeof phrases.success === "function" ? phrases.success(body) : phrases.success}`;
}

// allManualText is "6 人を手動にしました（未接続のため自動のまま: C、F）" for the
// skipped players of set_all_node_modes (admin-ui.md 11.1.1). The count
// includes the players that were manual already.
function allManualText(skipped) {
  const count = Math.max(PLAYER_IDS.length - skipped.length, 0);
  const left = skipped.length > 0 ? `（未接続のため自動のまま: ${skipped.map(displayName).join("、")}）` : "";
  return `${count} 人を手動にしました${left}`;
}

// errorTexts holds the wording of every code of web.md 5 (admin-ui.md 11.3
// and 11.4). page_test.go checks that no code of errors.go is missing.
export const errorTexts = {
  invalid_request: "要求の形式が正しくありません",
  not_found: "機器が見つかりません。一覧を確かめてください",
  method_not_allowed: "この操作は受け付けられません（画面の不具合の可能性があります）",
  phase_conflict: "現在の運転状態では実行できません",
  pid_exhausted: "次のゲームに必要なパケットIDが足りません。サーバーを再起動してください",
  players_not_ready: "準備完了していない手動プレイヤーがいます",
  assignment_conflict: "その割当先はほかの機器が使っています。その機器と入れ替えるか、先にその機器の割当を外してください",
  device_disconnected: "機器が接続されていません",
  device_kind_mismatch: "機器の種別が合いません。プレイヤー基板とLED制御基板の間では、割り当て・入れ替え・交代はできません",
  device_not_assigned: "割当のない機器が含まれているため、入れ替えや交代はできません",
  manual_mode_conflict: "手動のプレイヤーは確認表示できません。先に自動にしてください",
  unsupported_media_type: "要求の形式が正しくありません（画面の不具合の可能性があります）",
  // 500 and 503 are unknown results, not rejections (admin-ui.md 11.3).
  internal_error: "サーバー内部エラー",
  unavailable: "サーバーが要求を受け付けられない状態",
};

const assignmentTypes = ["set_device_assignment", "swap_device_assignments", "replace_device_assignment"];

// Rewordings of a code for some operations (admin-ui.md 11.4, 15.5).
// simultaneous is whether the screen of the simultaneous game sent it.
function rewording(command, code, simultaneous) {
  const type = command.type;
  switch (code) {
    case "invalid_request":
      if (type === "set_bot_rate") {
        return "処理速度は 0 より大きい数で指定してください";
      }
      if (type === "swap_device_assignments" || type === "replace_device_assignment") {
        return "異なる 2 台を選んでください";
      }
      return undefined;
    case "phase_conflict":
      if (!simultaneous) {
        switch (type) {
          case "start":
            return "すでに運転中のため開始できません";
          case "stop":
            return "停止中のため停止できません";
          case "return_to_bot":
            return "停止中のため操作できません";
          default:
            return undefined;
        }
      }
      if (type === "start") {
        return "準備中ではないため開始できません";
      }
      if (type === "finish") {
        return "ゲーム中ではないため終了できません";
      }
      if (type === "set_node_mode" || type === "set_all_node_modes" || assignmentTypes.includes(type)) {
        return "ゲーム中は変更できません。準備中か終了後に操作してください";
      }
      if (type === "set_device_preview") {
        return "ゲーム中は確認表示を始められません";
      }
      return undefined;
    case "device_disconnected":
      return type === "set_node_mode" ? "対応する機器が接続されていないため、手動にできません" : undefined;
    default:
      return undefined;
  }
}

export function isStringList(v) {
  return Array.isArray(v) && v.every((e) => typeof e === "string");
}

// rejectedText is the rejection of an error result (admin-ui.md 10), such as
// "× C（P3）を手動にできませんでした: …（device_disconnected）".
export function rejectedText(command, phrases, result) {
  const { code, details } = result;
  let reason = rewording(command, code, phrases.simultaneous === true) ?? (Object.hasOwn(errorTexts, code) ? errorTexts[code] : "拒否されました");
  if (code === "players_not_ready" && isStringList(details.nodeIds)) {
    reason += `: ${details.nodeIds.map(nodeLabel).join("、")}`;
  }
  return `× ${phrases.failure}: ${reason}（${code}）`;
}

const unknownReasons = {
  timeout: "タイムアウト",
  network: "通信エラー",
};

export const EXCEPTION_REASON = "画面の処理でエラーが起きました";

// unknownReason is the reason of admin-ui.md 11.3 for a result that is
// neither a success nor a rejection.
export function unknownReason(result) {
  if (result.kind === "error") {
    return Object.hasOwn(errorTexts, result.code) ? errorTexts[result.code] : `HTTP ${result.status}`;
  }
  if (result.reason === "bad_response") {
    return result.status ? `不正な応答 HTTP ${result.status}` : "不正な応答";
  }
  return unknownReasons[result.reason] ?? result.reason;
}

export function unknownText(phrases, reason) {
  return `? 「${phrases.name}」の結果は不明です（${reason}）。再送はしません。状態を確かめてください`;
}

// ---------- ベストスコアと記録ファイル（admin-ui.md 5.9、5.10、7.4、11.7） ----------

export const SAVE_FAILED_TEXT = "× 結果の保存に失敗しました。サーバーのログを確認してください。";

export function recordsUnavailableText(reason) {
  const why = reason ? `（${reason}）` : "";
  return `× 記録ファイルを読み込めませんでした${why}。この起動中の結果は保存されません。` +
    "ファイルを確認してからサーバーを再起動してください。";
}

export const BESTS_THIS_PROCESS = "この起動中のゲームだけで集計";
export const BESTS_SAVE_FAILED = "直近の結果を保存できませんでした";
export const BESTS_NOTE = "1時間ごと・通算は最後まで行ったゲームだけ";
export const ROUNDS_THIS_PROCESS = "この起動中のラウンドだけで集計";
export const ROUNDS_NOTE = "打ち切ったラウンドと練習は記録しません";
export const NO_ROUNDS = "ラウンドの記録はまだありません";
// Appended to the heading of the player table while the records file cannot
// be used and a result carries a top percent (admin-ui.md 5.3, 15.3).
export const TOP_THIS_PROCESS_ROUNDS = "上位%はこの起動中のラウンドだけで計算";
export const TOP_THIS_PROCESS_GAMES = "上位%はこの起動中のゲームだけで計算";
export const NO_ELIGIBLE = "ランキング対象者なし";
export const NO_RECORDS = "記録はまだありません";

// The times of web.md 3.2 are RFC 3339 in +09:00. Their date and time are
// read from the string and not converted to the zone of the browser, so
// that they are JST whatever the zone of the laptop (admin-ui.md 7.4).
const jstTime = /^\d{4}-(\d{2})-(\d{2})T(\d{2}):(\d{2}):\d{2}(?:\.\d+)?\+09:00$/;

function jstParts(time) {
  const m = jstTime.exec(time);
  return m && { date: `${Number(m[1])}/${Number(m[2])}`, hour: m[3], minute: m[4] };
}

// jstDate is "10/28", or null for a time not in the form of web.md 3.2.
export function jstDate(time) {
  return jstParts(time)?.date ?? null;
}

// hourLabel is "13時台", or "10/28 13時台" with the date. A time in another
// form is shown as it is.
export function hourLabel(hourStart, withDate) {
  const p = jstParts(hourStart);
  if (!p) {
    return hourStart;
  }
  const hour = `${Number(p.hour)}時台`;
  return withDate ? `${p.date} ${hour}` : hour;
}

// finishedLabel is "13:05", or "10/28 13:05" with the date.
export function finishedLabel(finishedAt, withDate) {
  const p = jstParts(finishedAt);
  if (!p) {
    return finishedAt;
  }
  const time = `${p.hour}:${p.minute}`;
  return withDate ? `${p.date} ${time}` : time;
}

// ---------- 機器（admin-ui.md 5.5、7.3） ----------

export function previewText(device, preview) {
  if (!device) {
    return `確認用 ${preview.nodeId}（機器一覧にない機器）`;
  }
  const target = targetLabel({ kind: device.kind, nodeId: preview.nodeId });
  return device.assignment ? `${target}を表示` : `${target}を一時表示（未割当）`;
}

export const NO_PREVIEW = "確認表示: なし";
export const NO_PREVIEW_HINT = "機器を選んで「確認表示を始める」を押すと、実機が表示名と色で点滅します";
export const NO_PREVIEW_PLAYING = "ゲーム中は確認表示を始められません";
export const NO_DEVICES = "識別済みの機器はありません。基板をUSBハブにつなぐと、ここに出ます";

// ---------- 選択中の機器（admin-ui.md 5.7、11.5） ----------

export const INSPECTOR_EMPTY = "機器の一覧から 1 台選ぶと、確認表示と割当の操作がここに出ます。";

export function missingSelectionText(deviceId) {
  return `選択していた機器 ${deviceId} は一覧にありません`;
}

export function inspectorPreviewNote(device, preview) {
  const target = targetLabel({ kind: device.kind, nodeId: preview.nodeId });
  return device.assignment
    ? `確認表示中: 実機が ${target} を点滅表示しています。`
    : `確認表示中: 実機は一時的に ${target} を点滅表示しています。割当ではありません。`;
}

export const RUNNING_ASSIGN_NOTE = "運転中は、手動のプレイヤーの機器の割当は変えられません";
export const ASSIGNED_HINT = "入れ替え: 2 台の割当先を取り替える。交代: 割当を相手に渡し、この機器は未割当になる。";
export const UNASSIGNED_HINT = "交代: 相手の割当をこの機器が引き継ぎ、相手は未割当になる。故障した機器を予備に替えるときに使う。";

export const choosePrompts = {
  target: "割当先を選んでください",
  swap: "入れ替える相手を選んでください",
  replace: "交代する相手を選んでください",
};

export const RATE_NOT_A_NUMBER = "処理速度を数で入力してください";

// ---------- 確認ダイアログ（admin-ui.md 11.6） ----------

function previewEndNote(deviceId) {
  return `確認表示中の ${deviceId} は確認表示を終了します。`;
}

const KEPT_BY_RESET = "機器の割当、操作モード、Botの処理速度はそのままです。";
const KEPT_BY_STOP = "機器の割当、操作モード、Botの処理速度、記録したラウンドはそのままです。";

// dialogText returns {title, paragraphs, confirm, danger} of a confirmation.
// Its values follow the latest snapshot while the dialog is open.
export function dialogText(kind, context, snapshot) {
  const preview = snapshot.preview;
  switch (kind) {
    case "stop":
      return stopText(snapshot.players, preview);
    case "return-to-bot":
      return returnToBotText(context.nodeId, snapshot.players);
    case "finish":
      return {
        title: "ゲームを終了しますか？",
        paragraphs: [
          `残り ${snapshot.game.remainingSeconds} 秒で終了し、この時点のスコアで結果を確定します。`,
          "終了したゲームは再開できません。次のゲームはリセットしてから始めます。",
        ],
        confirm: "ゲームを終了する",
        danger: true,
      };
    case "reset":
      return { ...resetText(snapshot.game.phase, preview), confirm: "リセットする", danger: true };
    case "all-auto":
      return allAutoText(snapshot.players, isSimultaneous(snapshot.game));
    case "swap":
      return swapText(context, snapshot);
    case "replace":
      return replaceText(context, snapshot);
    default:
      return { title: kind, paragraphs: [], confirm: "実行する", danger: false };
  }
}

function resetText(phase, preview) {
  let text;
  if (phase === "Playing") {
    text = {
      title: "進行中のゲームを中止してリセットしますか？",
      paragraphs: ["ゲームを中止し、スコア・パケット・残り時間を初期化します。全員の準備完了も解除されます。結果は確定しません。"],
    };
  } else if (phase === "Finished") {
    text = {
      title: "リセットして準備中に戻しますか？",
      paragraphs: [
        "このゲームの結果（スコアと順位）を消して、スコア・パケット・残り時間を初期化します。全員の準備完了も解除されます。",
        "結果はこの画面にもプレイヤーの画面にも残りません。必要なら先に記録してください。",
      ],
    };
  } else {
    text = {
      title: "リセットしますか？",
      paragraphs: ["スコア・パケット・残り時間を初期化し、全員の準備完了を解除します。"],
    };
  }
  text.paragraphs.push(KEPT_BY_RESET);
  if (preview) {
    text.paragraphs.push(previewEndNote(preview.deviceId));
  }
  return text;
}

// stopText is the confirmation of the stop (admin-ui.md 11.6).
function stopText(players, preview) {
  const counts = roundCounts(players);
  const paragraphs = [
    counts.normal + counts.tutorial > 0
      ? `進行中のラウンドは打ち切られ、結果は記録されません（通常 ${counts.normal} 人、練習 ${counts.tutorial} 人）。` +
        "全員が停止中に戻り、パケット・スコアを初期化します。"
      : "進行中のラウンドはありません。全員が停止中に戻り、パケット・スコアを初期化します。",
    KEPT_BY_STOP,
  ];
  if (preview) {
    paragraphs.push(previewEndNote(preview.deviceId));
  }
  return { title: "運転を停止しますか？", paragraphs, confirm: "運転を停止する", danger: true };
}

// returnToBotText is the confirmation of "戻す…": what is cut short depends
// on the station the player is in now (admin-ui.md 11.6).
function returnToBotText(nodeId, players) {
  const player = players.find((p) => p.nodeId === nodeId);
  const kept = "受付（手動）は続けます。";
  let body;
  if (player?.station === "select") {
    body = `モード選択を中止して待機に戻します。${kept}`;
  } else if (player?.station === "playing" && player.round === "tutorial") {
    body = `練習を終えて待機に戻します。${kept}`;
  } else if (player?.station === "playing") {
    body = `通常ラウンドを打ち切って待機に戻します。この結果は記録されません。${kept}`;
  } else if (player?.station === "finished") {
    body = `結果の表示を終えて待機に戻します。この結果は記録済みです。${kept}`;
  } else {
    body = `待機に戻します。${kept}`;
  }
  return { title: `${nodeLabel(nodeId)}をボットに戻しますか？`, paragraphs: [body], confirm: "ボットに戻す", danger: true };
}

function allAutoText(players, simultaneous) {
  const manual = players.filter((p) => p.mode === "manual").length;
  // The ready flags belong to the simultaneous game; endless operation cuts
  // the rounds short instead.
  const effect = simultaneous ? "準備完了が解除されます" : "進行中のラウンドは打ち切られます";
  return {
    title: "全員を自動にしますか？",
    paragraphs: [manual > 0 ? `手動の ${manual} 人が自動操作に切り替わり、${effect}。` : "すでに全員が自動です。"],
    confirm: "全員を自動にする",
    danger: false,
  };
}

function findDevice(devices, deviceId) {
  return devices.find((device) => device.deviceId === deviceId);
}

// switchNote says which player nodes become automatic when their board
// changes (admin-ui.md 11.6, 15.6), or "" when none does. In endless
// operation that happens only to a manual player while it is stopped; while
// it runs, app refuses to change the board of a manual player instead.
function switchNote(targets, snapshot, lead) {
  const labels = targets.filter((t) => t?.kind === "player").map(targetLabel);
  if (labels.length === 0) {
    return "";
  }
  const who = labels.join(" と ");
  if (isSimultaneous(snapshot.game)) {
    return `${lead}${who} は自動操作に切り替わり、準備完了が解除されます。`;
  }
  const manual = targets.some((t) => t?.kind === "player" &&
    snapshot.players.find((p) => p.nodeId === t.nodeId)?.mode === "manual");
  return snapshot.game.operation === "stopped" && manual ? `${lead}${who} は自動操作に切り替わります。` : "";
}

function swapText({ first, second }, snapshot) {
  const { devices, preview } = snapshot;
  const a = findDevice(devices, first);
  const b = findDevice(devices, second);
  const paragraphs = [];
  const note = a?.assignment && b?.assignment && a.kind === "player" ? switchNote([a.assignment, b.assignment], snapshot, "担当が変わる ") : "";
  if (note) {
    paragraphs.push(note);
  }
  paragraphs.push("もう一度入れ替えると元の割当に戻ります。");
  if (preview && (preview.deviceId === first || preview.deviceId === second)) {
    paragraphs.push(previewEndNote(preview.deviceId));
  }
  return {
    title: "2 台の割当を入れ替えますか？",
    after: "入れ替え後",
    rows: [
      { deviceId: first, device: a, now: a?.assignment ?? null, after: b?.assignment ?? null },
      { deviceId: second, device: b, now: b?.assignment ?? null, after: a?.assignment ?? null },
    ],
    paragraphs,
    confirm: "入れ替える",
    danger: false,
  };
}

function replaceText({ oldDeviceId, newDeviceId }, snapshot) {
  const { devices, preview } = snapshot;
  const oldDevice = findDevice(devices, oldDeviceId);
  const newDevice = findDevice(devices, newDeviceId);
  const slot = oldDevice?.assignment ?? null;
  const lost = newDevice?.assignment ?? null;
  const paragraphs = [`${oldDeviceId} は未割当になり、再接続しても自動では割り当てられません。`];
  if (lost) {
    paragraphs.push(`${newDeviceId} の今の割当 ${targetLabel(lost)} は外れます。`);
  }
  // Every player node whose board changes becomes auto (server.md 4.2.1).
  const note = switchNote([slot, lost], snapshot, "");
  if (note) {
    paragraphs.push(note);
  }
  if (preview && (preview.deviceId === oldDeviceId || preview.deviceId === newDeviceId)) {
    paragraphs.push(previewEndNote(preview.deviceId));
  }
  return {
    title: "機器を交代しますか？",
    after: "交代後",
    rows: [
      { deviceId: oldDeviceId, device: oldDevice, now: slot, after: null },
      { deviceId: newDeviceId, device: newDevice, now: lost, after: slot },
    ],
    paragraphs,
    confirm: "交代する",
    danger: false,
  };
}
