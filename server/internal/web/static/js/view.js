// Rendering of the GET /api/state snapshot (docs/spec/web.md 3). Every value
// from the server, Device IDs included, goes into the page through
// textContent or attributes, never through HTML. Wording is chosen from the
// codes and enum values; API messages are never parsed. The remaining time is
// shown as fetched, without counting down (web.md 6).

// Display names of docs/spec/player.md.
const displayNames = {
  P1: "A", P2: "B", P3: "C", P4: "D", P5: "E", P6: "F", P7: "G", P8: "H",
  B1: "I", B2: "J", B3: "K",
};

const phaseLabels = { Preparing: "準備中", Playing: "ゲーム中", Finished: "終了" };
const modeLabels = { manual: "手動", auto: "自動" };
const kindLabels = { player: "プレイヤー", led: "LED制御" };

// The order of web.md 3.3. A code unknown to this page is shown as is.
const blockerLabels = {
  not_preparing: () => "準備中のフェーズではありません",
  pid_exhausted: () => "次のゲームに必要な未発行のPIDが足りません",
  players_not_ready: (blocker) =>
    `準備完了していない手動プレイヤー: ${(blocker.nodeIds ?? []).map(nodeLabel).join("、")}`,
};

// BOT_FIFO_MAX is the capacity of a Bot FIFO (web.md 3.2).
const BOT_FIFO_MAX = 4;

function nodeLabel(nodeId) {
  return `${displayNames[nodeId] ?? "?"}（${nodeId}）`;
}

// setText leaves equal text alone, so that the live region announces only
// changes and a selection in unchanged text survives the next poll.
export function setText(element, text) {
  if (element.textContent !== text) {
    element.textContent = text;
  }
}

// renderGame fills the game section except the start blockers. els holds
// the elements of index.html.
export function renderGame(els, game) {
  els.phase.dataset.phase = game.phase;
  setText(els.phase, phaseLabels[game.phase] ?? game.phase);
  setText(els.remaining, `${game.remainingSeconds} 秒`);
  setText(els.duration, `/ ${game.durationSeconds} 秒`);
  els.canStart.dataset.canStart = String(game.canStart);
  setText(els.canStart, game.canStart ? "開始できます" : "開始できません");
}

export function renderBlockers(list, blockers) {
  list.replaceChildren(...blockers.map((blocker) => {
    const label = blockerLabels[blocker.code];
    return element("li", label ? label(blocker) : `開始できない理由（${blocker.code}）`);
  }));
}

export function renderPlayers(tbody, players, phase) {
  tbody.replaceChildren(...players.map((player) => {
    const row = element("tr");
    row.dataset.mode = player.mode;
    row.append(
      nameCell(player.nodeId),
      element("td", modeLabels[player.mode] ?? player.mode),
      readyCell(player, phase),
      element("td", String(player.fifoLength), "num"),
      element("td", String(player.score), "num"),
      eligibleCell(player.rankingEligible),
    );
    return row;
  }));
}

export function renderBots(tbody, bots) {
  tbody.replaceChildren(...bots.map((bot) => {
    const row = element("tr");
    row.append(nameCell(bot.nodeId), fifoCell(bot.fifoLength), element("td", `${bot.ratePps} pps`, "num"));
    return row;
  }));
}

// renderDevices fills the device table and the preview summary above it.
// The preview row is marked, and a preview of an unassigned device is shown
// as the temporary node it uses, since its assignment stays null (web.md 3.2).
export function renderDevices(els, devices, preview) {
  const target = preview && devices.find((d) => d.deviceId === preview.deviceId);
  if (!preview) {
    setText(els.preview, "確認表示: なし");
  } else {
    setText(els.preview, `確認表示: ${preview.deviceId} — ${previewLabel(target, preview)}`);
  }
  els.preview.dataset.active = String(Boolean(preview));
  if (devices.length === 0) {
    els.tbody.replaceChildren(placeholderRow("識別済みの機器はありません", 5));
    return;
  }
  els.tbody.replaceChildren(...devices.map((device) => {
    const row = element("tr");
    const previewed = device === target;
    row.classList.toggle("preview", previewed);
    row.append(
      element("td", device.deviceId, "device-id"),
      element("td", kindLabels[device.kind] ?? device.kind),
      connectedCell(device.connected),
      assignmentCell(device.assignment),
      element("td", previewed ? previewLabel(device, preview) : "", "preview-cell"),
    );
    return row;
  }));
}

// previewLabel describes the node a previewed device shows. device is
// undefined when the snapshot does not list the previewed device; then it is
// unknown whether the node is the device's assignment or a temporary one.
function previewLabel(device, preview) {
  if (!device) {
    return `確認用 ${preview.nodeId}（機器一覧にない機器）`;
  }
  const prefix = device.kind === "led" ? "LED " : "";
  if (device.assignment === null) {
    return `確認用 ${prefix}${preview.nodeId}（一時）`;
  }
  const node = device.kind === "player" ? nodeLabel(preview.nodeId) : `${prefix}${preview.nodeId}`;
  return `確認表示中 ${node}`;
}

function nameCell(nodeId) {
  const cell = element("th");
  cell.scope = "row";
  cell.append(element("span", displayNames[nodeId] ?? "?", "name"), " ", element("span", nodeId, "node-id"));
  return cell;
}

// ready means something only for manual players; auto ones are always false.
// It is a start condition only in Preparing, so it is highlighted only there.
function readyCell(player, phase) {
  if (player.mode !== "manual") {
    const cell = element("td", "—", "muted");
    cell.title = "自動操作では使用しません";
    return cell;
  }
  const text = player.ready ? "準備完了" : "未準備";
  if (phase !== "Preparing") {
    const cell = element("td", text, "muted");
    cell.title = "開始の条件になるのは準備中だけです";
    return cell;
  }
  return element("td", text, player.ready ? "ok" : "warn");
}

function eligibleCell(eligible) {
  if (eligible === null) {
    const cell = element("td", "—", "muted");
    cell.title = "Preparingでは未確定です";
    return cell;
  }
  return eligible ? element("td", "対象") : element("td", "対象外", "warn");
}

function fifoCell(length) {
  const meter = element("span", "", "meter");
  meter.setAttribute("aria-hidden", "true");
  for (let i = 0; i < BOT_FIFO_MAX; i++) {
    meter.append(element("span", "", i < length ? "slot filled" : "slot"));
  }
  const fifo = element("span", "", "fifo");
  fifo.append(meter, element("span", `${length} / ${BOT_FIFO_MAX}`, "num"));
  const cell = element("td");
  cell.append(fifo);
  return cell;
}

function connectedCell(connected) {
  return connected ? element("td", "接続", "ok") : element("td", "切断", "bad");
}

function assignmentCell(assignment) {
  if (assignment === null) {
    return element("td", "未割当", "unassigned");
  }
  return element("td", assignment.kind === "led" ? `LED ${assignment.nodeId}` : nodeLabel(assignment.nodeId));
}

function placeholderRow(text, columns) {
  const cell = element("td", text, "placeholder");
  cell.colSpan = columns;
  const row = element("tr");
  row.append(cell);
  return row;
}

function element(tag, text = "", className = "") {
  const e = document.createElement(tag);
  e.textContent = text;
  if (className) {
    e.className = className;
  }
  return e;
}
