// Admin page. It shows the latest snapshot and the connection state, and
// keeps every operation disabled until the first GET succeeds and while the
// state cannot be fetched (docs/spec/web.md 6).

import { Poller } from "./poller.js";

const ui = {
  connection: document.getElementById("connection"),
  connectionDetail: document.getElementById("connection-detail"),
  updated: document.getElementById("updated"),
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

function canOperate() {
  return page.connection === "online";
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
  ui.operations.disabled = !canOperate();
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

renderStatus();
poller.start();
