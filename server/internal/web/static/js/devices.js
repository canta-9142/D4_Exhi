// The device list and the preview bar above it (docs/spec/admin-ui.md 5.5,
// 7.3), and the Device ID of the selected device. Rows are keyed by Device
// ID: new devices are inserted where the API order puts them, gone ones are
// removed, and the others only have their cells updated.

import { chip, element, placeChildren, setAttr, setContent, setText, tile } from "./dom.js";
import {
  NO_DEVICES, NO_PREVIEW, NO_PREVIEW_HINT, NO_PREVIEW_PLAYING, TARGETS, displayName, kindLabel, previewText,
  targetLabel,
} from "./labels.js";

const ui = {
  meta: document.getElementById("devices-meta"),
  previewBar: document.getElementById("preview-bar"),
  previewState: document.getElementById("preview-state"),
  previewDevice: document.getElementById("preview-device"),
  previewWhat: document.getElementById("preview-what"),
  previewStop: document.getElementById("preview-stop"),
  tbody: document.getElementById("devices"),
};

// The kinds in the order of their groups. Kinds unknown to the page follow
// in a group of their own, named as they are.
const KINDS = ["player", "led"];

const rows = new Map();
const groupRows = new Map();
let emptyRow = null;

// selected is the Device ID of the selected device, kept across polls.
let selected = null;
let onSelect = () => {};

export function selectedDeviceId() {
  return selected;
}

// install sets the callback called with the new Device ID, or null, when the
// selection changes.
export function install(callback) {
  onSelect = callback;
  ui.tbody.addEventListener("change", (event) => {
    if (event.target.name === "device") {
      select(event.target.value);
    }
  });
  // A click anywhere on a row selects it, like a click on its radio. The
  // disabled fieldset disables this too.
  ui.tbody.addEventListener("click", (event) => {
    const row = event.target.closest("tr[data-device]");
    const radio = row?.querySelector("input[type=radio]");
    if (radio && !radio.matches(":disabled") && event.target !== radio) {
      radio.checked = true;
      select(row.dataset.device);
    }
  });
}

export function select(deviceId) {
  if (selected === deviceId) {
    return;
  }
  selected = deviceId;
  for (const [id, row] of rows) {
    markSelected(row, id === selected);
  }
  onSelect(selected);
}

// renderDevices updates the list and returns the Device ID of a selected
// device that is no longer listed, after dropping the selection, or null.
export function renderDevices(snapshot) {
  const { devices, preview, game } = snapshot;
  renderMeta(devices);
  renderPreviewBar(devices, preview, game.phase);

  let missing = null;
  if (selected !== null && !devices.some((d) => d.deviceId === selected)) {
    missing = selected;
    selected = null;
  }
  if (devices.length === 0) {
    emptyRow ??= placeholderRow(NO_DEVICES);
    placeChildren(ui.tbody, [emptyRow]);
    rows.clear();
    return missing;
  }

  const kinds = [...KINDS, ...new Set(devices.map((d) => d.kind).filter((k) => !KINDS.includes(k)))];
  const listed = new Set();
  const children = [];
  for (const kind of kinds) {
    const group = devices.filter((d) => d.kind === kind);
    if (group.length === 0) {
      continue;
    }
    children.push(groupRow(kind, group));
    for (const device of group) {
      let row = rows.get(device.deviceId);
      if (!row) {
        row = createRow(device.deviceId);
        rows.set(device.deviceId, row);
      }
      updateRow(row, device, preview);
      listed.add(device.deviceId);
      children.push(row);
    }
  }
  for (const id of rows.keys()) {
    if (!listed.has(id)) {
      rows.delete(id);
    }
  }
  placeChildren(ui.tbody, children);
  return missing;
}

// The heading shows the targets in use and the unassigned devices apart,
// not the total, which spare boards make larger than the number of nodes.
function renderMeta(devices) {
  const lost = devices.filter((d) => d.assignment && !d.connected).length;
  const targets = TARGETS.player.length + TARGETS.led.length;
  const live = devices.filter((d) => d.assignment && d.connected).length;
  setText(ui.meta, lost > 0 ? `割当済みの機器が ${lost} 台切断しています` : `割当先 ${live} / ${targets} が接続済み`);
  ui.meta.classList.toggle("warn", lost > 0 || live < targets);
}

function renderPreviewBar(devices, preview, phase) {
  ui.previewBar.dataset.active = String(Boolean(preview));
  ui.previewStop.hidden = !preview;
  if (!preview) {
    setText(ui.previewState, NO_PREVIEW);
    setText(ui.previewDevice, "");
    setText(ui.previewWhat, phase === "Playing" ? NO_PREVIEW_PLAYING : NO_PREVIEW_HINT);
    return;
  }
  const device = devices.find((d) => d.deviceId === preview.deviceId);
  setText(ui.previewState, "◎ 確認表示中");
  setText(ui.previewDevice, preview.deviceId);
  setText(ui.previewWhat, previewText(device, preview));
}

function groupRow(kind, group) {
  let row = groupRows.get(kind);
  if (!row) {
    const head = element("th");
    head.colSpan = 5;
    head.scope = "rowgroup";
    head.append(element("b", kindLabel(kind)), element("span"));
    row = element("tr", "", "group");
    row.append(head);
    groupRows.set(kind, row);
  }
  const lostOf = (list) => list.filter((d) => !d.connected).length;
  const assigned = group.filter((d) => d.assignment);
  const spare = group.filter((d) => !d.assignment);
  const targets = TARGETS[kind]?.length;
  let text = targets === undefined ? `　${group.length} 台` : `　割当先 ${assigned.length} / ${targets}`;
  if (targets !== undefined && lostOf(assigned) > 0) {
    text += `（うち切断 ${lostOf(assigned)}）`;
  }
  if (targets !== undefined && spare.length > 0) {
    text += `　未割当 ${spare.length} 台${lostOf(spare) > 0 ? `（うち切断 ${lostOf(spare)}）` : ""}`;
  }
  setText(row.cells[0].lastChild, text);
  return row;
}

function createRow(deviceId) {
  const row = element("tr", "", "row");
  row.dataset.device = deviceId;
  const radio = element("input");
  radio.type = "radio";
  radio.name = "device";
  radio.value = deviceId;
  radio.setAttribute("aria-label", `${deviceId} を選ぶ`);
  const choose = element("td");
  choose.append(radio);
  row.append(choose, element("td", deviceId, "dev-id"), element("td"), element("td"), element("td"));
  markSelected(row, deviceId === selected);
  return row;
}

function updateRow(row, device, preview) {
  const [, , connectedCell, assignmentCell, previewCell] = row.cells;
  const lost = device.assignment && !device.connected;
  row.classList.toggle("lost", Boolean(lost));
  row.classList.toggle("idle", !device.assignment && !device.connected);
  setText(connectedCell, device.connected ? "● 接続" : "× 切断");
  setAttr(connectedCell, "class", device.connected ? "ok" : lost ? "bad" : null);

  const a = device.assignment;
  setContent(assignmentCell, a ? `${a.kind}:${a.nodeId}` : `none:${device.connected}`, () => [
    a ? chip(a.nodeId, displayName(a.nodeId), targetLabel(a), a.kind === "led" ? "led" : "")
      : chip(null, "", "未割当", "empty", device.connected ? "warn" : ""),
  ]);

  // The blinking parts are rebuilt only when the preview changes, which
  // keeps the 1-second cycle running across polls (admin-ui.md 7.3).
  const previewed = preview?.deviceId === device.deviceId;
  const key = !previewed ? "" : a ? "assigned" : `temp:${device.kind}:${preview.nodeId}`;
  setContent(previewCell, key, () => {
    if (!previewed) {
      return [];
    }
    if (a) {
      return [element("span", "◎ 確認表示中", "pv blink")];
    }
    const temp = element("span", "", "node");
    temp.append(tile(preview.nodeId, displayName(preview.nodeId), "temp blink", true),
      element("span", `◎ 一時 ${preview.nodeId}`, "pv"));
    return [temp];
  });
}

function markSelected(row, on) {
  row.classList.toggle("selected", on);
  row.querySelector("input[type=radio]").checked = on;
}

function placeholderRow(text) {
  const cell = element("td", text, "placeholder");
  cell.colSpan = 5;
  const row = element("tr");
  row.append(cell);
  return row;
}
