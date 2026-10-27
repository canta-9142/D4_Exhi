// Admin page. It shows the latest snapshot and the connection state, and
// keeps every operation disabled until the first GET succeeds, while the
// state cannot be fetched, and while an operation is in progress
// (docs/spec/web.md 6).

import { postCommand } from "./api.js";
import { Poller } from "./poller.js";

const ui = {
  connection: document.getElementById("connection"),
  connectionDetail: document.getElementById("connection-detail"),
  updated: document.getElementById("updated"),
  busy: document.getElementById("busy"),
  notice: document.getElementById("notice"),
  operations: document.getElementById("operations"),
  game: document.getElementById("game-json"),
  players: document.getElementById("players-json"),
  bots: document.getElementById("bots-json"),
  devices: document.getElementById("devices-json"),
};

const page = {
  // "connecting" until the first GET completes, then "online" or "offline".
  connection: "connecting",
  failure: null,
  busy: false,
};

const poller = new Poller({
  onSnapshot(snapshot) {
    page.connection = "online";
    page.failure = null;
    showSnapshot(snapshot);
    renderStatus();
  },
  onFailure(result) {
    page.connection = "offline";
    page.failure = result;
    renderStatus();
  },
});

// runCommand sends one admin operation (web.md 4) and resolves to its api.js
// result, or to null when operations are disabled. Every operation stays
// disabled until a GET sent after the result has updated the page. The
// operation is never resent: after a timeout, a network failure, 500 or 503
// its result is unknown, and the administrator decides from the new state.
export async function runCommand(command) {
  if (!canOperate()) {
    return null;
  }
  page.busy = true;
  showNotice("pending", `「${command.type}」を送信しています。`);
  renderStatus();
  const result = await postCommand(command);
  showResult(command, result);
  await poller.refresh();
  page.busy = false;
  renderStatus();
  return result;
}

function canOperate() {
  return page.connection === "online" && !page.busy;
}

function showSnapshot(snapshot) {
  ui.game.textContent = toJSON(snapshot.game);
  ui.players.textContent = toJSON(snapshot.players);
  ui.bots.textContent = toJSON(snapshot.bots);
  ui.devices.textContent = toJSON({ devices: snapshot.devices, preview: snapshot.preview });
  ui.updated.textContent = `最終取得 ${new Date().toLocaleTimeString("ja-JP")}`;
}

function toJSON(value) {
  return JSON.stringify(value, null, 2);
}

const connectionLabels = { connecting: "接続中", online: "正常", offline: "通信途絶" };

function renderStatus() {
  document.body.dataset.connection = page.connection;
  setText(ui.connection, connectionLabels[page.connection]);
  setText(ui.connectionDetail, page.failure ? describeFailure(page.failure) : "");
  ui.busy.hidden = !page.busy;
  ui.operations.disabled = !canOperate();
  ui.operations.setAttribute("aria-busy", String(page.busy));
}

// setText leaves equal text alone, so that the live region announces only
// changes and not every poll.
function setText(element, text) {
  if (element.textContent !== text) {
    element.textContent = text;
  }
}

const failureReasons = {
  timeout: "タイムアウト",
  network: "通信エラー",
  bad_response: "不正な応答",
};

function describeFailure(result) {
  if (result.kind === "error") {
    return `${result.message}（${result.code}）`;
  }
  const status = result.status ? ` HTTP ${result.status}` : "";
  return `${failureReasons[result.reason]}${status}`;
}

// After these error responses the operation may have been applied (web.md
// 6). 503, for example, also comes when app stops after receiving it.
const unknownStatuses = [500, 503];

// showResult shows the outcome of a POST until the next operation. Only the
// code and the status decide the outcome; the message is just displayed.
function showResult(command, result) {
  const name = `「${command.type}」`;
  if (result.kind === "success") {
    showNotice("success", `${name}を受け付けました。`);
  } else if (result.kind === "error" && !unknownStatuses.includes(result.status)) {
    showNotice("rejected", `${name}は拒否されました: ${describeFailure(result)}`);
  } else {
    showNotice("unknown", `${name}の結果は不明です（${describeFailure(result)}）。` +
      "自動では再送しません。再取得した状態を確認し、必要なら操作し直してください。");
  }
}

function showNotice(level, text) {
  ui.notice.dataset.level = level;
  ui.notice.textContent = text;
  ui.notice.hidden = false;
}

renderStatus();
poller.start();
