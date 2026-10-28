// Admin page. It shows the latest snapshot and the connection state, and
// keeps every operation disabled until the first GET succeeds, while the
// state cannot be fetched, and while an operation is in progress
// (docs/spec/web.md 6).

import { postCommand } from "./api.js";
import { Poller } from "./poller.js";
import { renderBlockers, renderBots, renderDevices, renderGame, renderPlayers, setText } from "./view.js";

const ui = {
  connection: document.getElementById("connection"),
  connectionDetail: document.getElementById("connection-detail"),
  updated: document.getElementById("updated"),
  busy: document.getElementById("busy"),
  notice: document.getElementById("notice"),
  stale: document.getElementById("stale"),
  operations: document.getElementById("operations"),
  game: {
    phase: document.getElementById("game-phase"),
    remaining: document.getElementById("game-remaining"),
    duration: document.getElementById("game-duration"),
    canStart: document.getElementById("game-can-start"),
  },
  blockers: document.getElementById("game-blockers"),
  players: document.getElementById("players-rows"),
  bots: document.getElementById("bots-rows"),
  devices: {
    preview: document.getElementById("preview-summary"),
    tbody: document.getElementById("devices-rows"),
  },
};

const page = {
  // "connecting" until the first GET completes, then "online" or "offline".
  connection: "connecting",
  failure: null,
  busy: false,
  // The JSON of the values each section shows, or undefined before the first
  // snapshot.
  shown: {},
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
// An exception in the page is rethrown after the same refetch.
export async function runCommand(command) {
  if (!canOperate()) {
    return null;
  }
  page.busy = true;
  try {
    showNotice("pending", `「${command.type}」を送信しています。`);
    renderStatus();
    const result = await postCommand(command);
    showResult(command, result);
    return result;
  } catch (err) {
    // The POST may have been sent before the exception.
    showNotice("unknown", `操作の結果は不明です（画面の処理でエラーが起きました）。${unknownAdvice}`);
    throw err;
  } finally {
    // Even after an exception, the operations are enabled again only once
    // the refetch has updated the page, since the POST may have been sent.
    await poller.refresh();
    page.busy = false;
    renderStatus();
  }
}

function canOperate() {
  return page.connection === "online" && !page.busy;
}

function showSnapshot(snapshot) {
  showSection("game", snapshot.game, () => renderGame(ui.game, snapshot.game));
  // The blockers are a section of their own, since the game values change
  // every second while Playing.
  showSection("blockers", snapshot.game.startBlockers,
    () => renderBlockers(ui.blockers, snapshot.game.startBlockers));
  showSection("players", { players: snapshot.players, phase: snapshot.game.phase },
    () => renderPlayers(ui.players, snapshot.players, snapshot.game.phase));
  showSection("bots", snapshot.bots, () => renderBots(ui.bots, snapshot.bots));
  showSection("devices", { devices: snapshot.devices, preview: snapshot.preview },
    () => renderDevices(ui.devices, snapshot.devices, snapshot.preview));
  ui.updated.textContent = `最終取得 ${new Date().toLocaleTimeString("ja-JP")}`;
}

// showSection renders a section only when its values changed, so that a
// selection in a table, such as a Device ID being copied, survives the polls.
function showSection(name, values, render) {
  const json = JSON.stringify(values);
  if (page.shown[name] !== json) {
    render();
    page.shown[name] = json;
  }
}

const connectionLabels = { connecting: "接続中", online: "正常", offline: "通信途絶" };

function renderStatus() {
  document.body.dataset.connection = page.connection;
  setText(ui.connection, connectionLabels[page.connection]);
  setText(ui.connectionDetail, page.failure ? describeFailure(page.failure) : "");
  // Until the first snapshot, the sections say that nothing is fetched yet.
  ui.stale.hidden = page.connection !== "offline" || page.shown.game === undefined;
  ui.busy.hidden = !page.busy;
  ui.operations.disabled = !canOperate();
  ui.operations.setAttribute("aria-busy", String(page.busy));
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

const unknownAdvice = "自動では再送しません。再取得した状態を確認し、必要なら操作し直してください。";

// showResult shows the outcome of a POST until the next operation. Only the
// code and the status decide the outcome; the message is just displayed.
function showResult(command, result) {
  const name = `「${command.type}」`;
  if (result.kind === "success") {
    showNotice("success", `${name}を受け付けました。`);
  } else if (result.kind === "error" && !unknownStatuses.includes(result.status)) {
    showNotice("rejected", `${name}は拒否されました: ${describeFailure(result)}`);
  } else {
    showNotice("unknown", `${name}の結果は不明です（${describeFailure(result)}）。${unknownAdvice}`);
  }
}

function showNotice(level, text) {
  ui.notice.dataset.level = level;
  ui.notice.textContent = text;
  ui.notice.hidden = false;
}

renderStatus();
poller.start();
