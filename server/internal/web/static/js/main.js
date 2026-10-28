// Admin page. It shows the latest snapshot and the connection state, and
// keeps every operation disabled until the first GET succeeds, while the
// state cannot be fetched, and while an operation is in progress
// (docs/spec/web.md 6). The sections are drawn by the other modules
// (docs/spec/admin-ui.md 13); this one starts them and owns runCommand and
// the status bar.

import * as actions from "./actions.js";
import { postCommand } from "./api.js";
import { renderDevices, install as installDevices, select, selectedDeviceId } from "./devices.js";
import { updateDialog } from "./dialogs.js";
import { setText } from "./dom.js";
import { renderInspector } from "./inspector.js";
import {
  EXCEPTION_REASON, commandPhrases, connectionLabels, fetchedText, pendingText, rejectedText, successText,
  unknownReason, unknownText,
} from "./labels.js";
import { renderMap } from "./map.js";
import { Poller } from "./poller.js";
import { renderBots, renderGame, renderPlayers } from "./view.js";

const ui = {
  connection: document.getElementById("connection"),
  fetched: document.getElementById("fetched"),
  busy: document.getElementById("busy"),
  notice: document.getElementById("notice"),
  operations: document.getElementById("operations"),
};

const page = {
  // "connecting" until the first GET completes, then "online" or "offline".
  connection: "connecting",
  busy: false,
  // The snapshot on screen and the time it was fetched, or null before the
  // first one. A failed GET keeps them.
  snapshot: null,
  fetchedAt: null,
  // The Device ID of a selected device that left the list, reported in the
  // panel until another device is selected or it is listed again.
  missing: null,
};

const poller = new Poller({
  onSnapshot(snapshot) {
    page.connection = "online";
    page.snapshot = snapshot;
    page.fetchedAt = new Date().toLocaleTimeString("ja-JP", { hour12: false });
    showSnapshot();
    renderStatus();
  },
  onFailure() {
    page.connection = "offline";
    renderStatus();
  },
});

// runCommand sends one admin operation (web.md 4) and resolves to its api.js
// result, or to null when operations are disabled. Every operation stays
// disabled until a GET sent after the result has updated the page. The
// operation is never resent: after a timeout, a network failure, 500 or 503
// its result is unknown, and the administrator decides from the new state.
// An exception in the page is rethrown after the same refetch. phrases is
// the wording of the result (labels.js commandPhrases).
export async function runCommand(command, phrases = commandPhrases(command)) {
  if (!canOperate()) {
    return null;
  }
  page.busy = true;
  try {
    showNotice("pending", pendingText(phrases));
    renderStatus();
    const result = await postCommand(command);
    showResult(command, phrases, result);
    return result;
  } catch (err) {
    // The POST may have been sent before the exception.
    showNotice("unknown", unknownText(phrases, EXCEPTION_REASON));
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

function showSnapshot() {
  const snapshot = page.snapshot;
  renderGame(snapshot);
  renderPlayers(snapshot);
  renderBots(snapshot);
  const missing = renderDevices(snapshot);
  if (missing !== null) {
    page.missing = missing;
  } else if (snapshot.devices.some((d) => d.deviceId === page.missing)) {
    // The panel would say that a listed device is not listed. The selection
    // stays dropped, as admin-ui.md 5.7 says.
    page.missing = null;
  }
  showSelection();
  updateDialog(snapshot);
}

// showSelection draws the parts that depend on the selected device.
function showSelection() {
  renderInspector(page.snapshot, selectedDeviceId(), page.missing);
  renderMap(page.snapshot, selectedDeviceId());
}

function renderStatus() {
  document.body.dataset.connection = page.connection;
  // Only the sections of a fetched snapshot are shown as stale.
  document.body.dataset.stale = String(page.connection === "offline" && page.snapshot !== null);
  setText(ui.connection, connectionLabels[page.connection]);
  setText(ui.fetched, fetchedText(page.connection, page.fetchedAt));
  ui.busy.hidden = !page.busy;
  ui.operations.disabled = !canOperate();
  ui.operations.setAttribute("aria-busy", String(page.busy));
}

// After these error responses the operation may have been applied (web.md
// 6). 503, for example, also comes when app stops after receiving it.
const unknownStatuses = [500, 503];

// showResult shows the outcome of a POST until the next operation. Only the
// code and the status decide the outcome; the message is never shown.
function showResult(command, phrases, result) {
  if (result.kind === "success") {
    showNotice("success", successText(phrases));
  } else if (result.kind === "error" && !unknownStatuses.includes(result.status)) {
    showNotice("rejected", rejectedText(command, phrases, result));
  } else {
    showNotice("unknown", unknownText(phrases, unknownReason(result)));
  }
}

function showNotice(level, text) {
  ui.notice.dataset.level = level;
  setText(ui.notice, text);
}

installDevices(() => {
  page.missing = null;
  if (page.snapshot) {
    showSelection();
  }
});
actions.install(ui.operations, {
  runCommand,
  getSnapshot: () => page.snapshot,
  getSelection: selectedDeviceId,
  select,
});
renderStatus();
poller.start();
