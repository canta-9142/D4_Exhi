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
const modeLabels = { manual: "手動", auto: "自動" };
const kindLabels = { player: "プレイヤー基板", led: "LED制御基板" };

export const PHASES = Object.keys(phaseLabels);

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

export function modeLabel(mode) {
  return modeLabels[mode] ?? mode;
}

export function kindLabel(kind) {
  return kindLabels[kind] ?? kind;
}

// ---------- 開始できない理由（admin-ui.md 11.2） ----------

const blockerTexts = {
  not_preparing: "準備中のフェーズではありません",
  pid_exhausted: "次のゲームに必要なパケットIDが足りません。サーバーを再起動してください",
  players_not_ready: "準備完了していない手動プレイヤー",
};

export function blockerText(code) {
  return blockerTexts[code] ?? `開始できない理由（${code}）`;
}

export const NO_MANUAL_HINT = "手動のプレイヤーがいないため、全員自動で始まります";
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
// rejection. devices is the snapshot the command was made from.
export function commandPhrases(command, devices = []) {
  const d = command.data ?? {};
  const make = (name, success, failure) => ({ name, success, failure });
  switch (command.type) {
    case "start":
      return make("ゲームの開始", "ゲームを開始しました", "ゲームを開始できませんでした");
    case "finish":
      return make("ゲームの終了", "ゲームを終了しました", "ゲームを終了できませんでした");
    case "reset":
      return make("リセット", "リセットしました。準備中に戻りました", "リセットできませんでした");
    case "set_node_mode": {
      const node = nodeLabel(d.nodeId), mode = modeLabel(d.mode);
      return make(`${node}の${mode}への切り替え`, `${node}を${mode}にしました`, `${node}を${mode}にできませんでした`);
    }
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

export function successText(phrases) {
  return `✓ ${phrases.success}`;
}

// errorTexts holds the wording of every code of web.md 5 (admin-ui.md 11.3
// and 11.4). page_test.go checks that no code of errors.go is missing.
export const errorTexts = {
  invalid_request: "要求の形式が正しくありません",
  not_found: "機器が見つかりません。一覧を確かめてください",
  method_not_allowed: "この操作は受け付けられません（画面の不具合の可能性があります）",
  phase_conflict: "現在のフェーズでは実行できません",
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

// Rewordings of a code for some operations (admin-ui.md 11.4).
function rewording(command, code) {
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
      if (type === "start") {
        return "準備中ではないため開始できません";
      }
      if (type === "finish") {
        return "ゲーム中ではないため終了できません";
      }
      if (type === "set_node_mode" || assignmentTypes.includes(type)) {
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

function isStringList(v) {
  return Array.isArray(v) && v.every((e) => typeof e === "string");
}

// rejectedText is the rejection of an error result (admin-ui.md 10), such as
// "× C（P3）を手動にできませんでした: …（device_disconnected）".
export function rejectedText(command, phrases, result) {
  const { code, details } = result;
  let reason = rewording(command, code) ?? (Object.hasOwn(errorTexts, code) ? errorTexts[code] : "拒否されました");
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

// dialogText returns {title, paragraphs, confirm, danger} of a confirmation.
// Its values follow the latest snapshot while the dialog is open.
export function dialogText(kind, context, snapshot) {
  const preview = snapshot.preview;
  switch (kind) {
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
    case "swap":
      return swapText(context, snapshot.devices, preview);
    case "replace":
      return replaceText(context, snapshot.devices, preview);
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

function findDevice(devices, deviceId) {
  return devices.find((device) => device.deviceId === deviceId);
}

function swapText({ first, second }, devices, preview) {
  const a = findDevice(devices, first);
  const b = findDevice(devices, second);
  const paragraphs = [];
  if (a?.assignment && b?.assignment && a.kind === "player") {
    paragraphs.push(`担当が変わる ${targetLabel(a.assignment)} と ${targetLabel(b.assignment)} は自動操作に切り替わり、準備完了が解除されます。`);
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

function replaceText({ oldDeviceId, newDeviceId }, devices, preview) {
  const oldDevice = findDevice(devices, oldDeviceId);
  const newDevice = findDevice(devices, newDeviceId);
  const slot = oldDevice?.assignment ?? null;
  const lost = newDevice?.assignment ?? null;
  const paragraphs = [`${oldDeviceId} は未割当になり、再接続しても自動では割り当てられません。`];
  if (lost) {
    paragraphs.push(`${newDeviceId} の今の割当 ${targetLabel(lost)} は外れます。`);
  }
  // Every player node whose board changes becomes auto (server.md 4.2.1).
  const changed = [slot, lost].filter((target) => target?.kind === "player").map(targetLabel);
  if (changed.length > 0) {
    paragraphs.push(`${changed.join(" と ")} は自動操作に切り替わり、準備完了が解除されます。`);
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
