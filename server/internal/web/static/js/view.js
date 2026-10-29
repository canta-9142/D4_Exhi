// The command bar, the player table and the Bot table (docs/spec/admin-ui.md
// 5.2-5.4). The table rows are made once and then updated cell by cell, so
// that the polls keep the focus and a typed Bot rate. The remaining time and
// the scores are shown as fetched, without counting down or adding points
// (web.md 6).

import { chip, element, fieldError, placeChildren, setAttr, setContent, setText, tile } from "./dom.js";
import {
  BOT_FIFO_MAX, COUNTDOWN_LABEL, FINISHED_HINT, NO_MANUAL_HINT, PHASES, REMAINING_LABEL, blockerText, displayName,
  modeLabel, nodeLabel, phaseLabel,
} from "./labels.js";

const ui = {
  phase: document.getElementById("phase"),
  track: document.getElementById("phase-track"),
  timeLabel: document.getElementById("time-label"),
  remaining: document.getElementById("remaining"),
  duration: document.getElementById("duration"),
  timeBar: document.getElementById("time-bar"),
  readiness: document.getElementById("readiness"),
  blockers: document.getElementById("blockers"),
  startHint: document.getElementById("start-hint"),
  start: document.getElementById("start"),
  resetNext: document.getElementById("reset-next"),
  reset: document.getElementById("reset"),
  finish: document.getElementById("finish"),
  playersMeta: document.getElementById("players-meta"),
  rankHead: document.getElementById("rank-head"),
  players: document.getElementById("players"),
  bots: document.getElementById("bots"),
};

const leadHeads = document.querySelectorAll("th[data-lead]");

export function renderGame(snapshot) {
  const { game, players } = snapshot;
  document.body.dataset.phase = game.phase;
  setText(ui.phase, phaseLabel(game.phase));
  for (const step of ui.track.children) {
    setAttr(step, "aria-current", step.dataset.phase === game.phase ? "step" : null);
  }
  // The countdown before the game time takes the place of the remaining time;
  // the phase stays Playing (admin-ui.md 5.2).
  const countdown = game.countdownSeconds > 0;
  document.body.dataset.countdown = String(countdown);
  setText(ui.timeLabel, countdown ? COUNTDOWN_LABEL : REMAINING_LABEL);
  setText(ui.remaining, String(countdown ? game.countdownSeconds : game.remainingSeconds));
  setText(ui.duration, countdown ? "" : `/ ${game.durationSeconds} 秒`);
  const ratio = game.durationSeconds > 0 ? Math.min(Math.max(game.remainingSeconds / game.durationSeconds, 0), 1) : 0;
  ui.timeBar.style.width = `${ratio * 100}%`;

  // Outside Preparing, not being able to start is the normal state, so a
  // lone not_preparing is not shown as an alert (admin-ui.md 5.2).
  const quiet = !game.canStart && game.startBlockers.every((b) => b.code === "not_preparing");
  ui.readiness.dataset.canStart = quiet ? "quiet" : String(game.canStart);
  setText(ui.readiness, game.canStart ? "✓ 開始できます" : "開始できない理由");
  setContent(ui.blockers, JSON.stringify(game.startBlockers), () => game.startBlockers.map(blockerItem));
  let hint = "";
  if (game.canStart && !players.some((p) => p.mode === "manual")) {
    hint = NO_MANUAL_HINT;
  } else if (game.phase === "Finished") {
    hint = FINISHED_HINT;
  }
  setText(ui.startHint, hint);
  ui.startHint.hidden = hint === "";

  // Only canStart and the phase decide what can be pressed (admin-ui.md 8.3).
  ui.start.disabled = !game.canStart;
  // An unknown phase disables nothing and leaves the decision to app.
  ui.finish.disabled = PHASES.includes(game.phase) && game.phase !== "Playing";

  // After a game the reset takes the place of the start button, and the small
  // one goes, so that the same operation is not shown twice (admin-ui.md 5.2).
  const finished = game.phase === "Finished";
  ui.start.hidden = finished;
  ui.resetNext.hidden = !finished;
  ui.reset.classList.toggle("away", finished);
}

function blockerItem(blocker) {
  const item = element("li", "", blocker.code === "not_preparing" ? "quiet" : "");
  item.append(element("span", blockerText(blocker.code)));
  if (blocker.code === "players_not_ready") {
    for (const nodeId of blocker.nodeIds ?? []) {
      item.append(chip(nodeId, displayName(nodeId), nodeLabel(nodeId)));
    }
  }
  return item;
}

// ---------- プレイヤー表 ----------

const playerRows = new Map();

export function renderPlayers(snapshot) {
  const { game, players, devices, records } = snapshot;
  const phase = game.phase;
  for (const head of leadHeads) {
    head.classList.toggle("lead", head.dataset.lead === phase);
  }
  setText(ui.rankHead, phase === "Finished" ? "順位" : "ランキング");
  const manual = players.filter((p) => p.mode === "manual");
  let meta = `手動 ${manual.length} 人`;
  if (phase === "Preparing") {
    meta += `、うち準備完了 ${manual.filter((p) => p.ready).length} 人`;
  } else if (phase === "Playing") {
    meta = "ゲーム中は操作モードを変更できません";
  } else if (phase === "Finished" && !records.available && players.some((p) => p.topPercent !== null)) {
    // The percentages then count only this run's games, as the best scores do.
    meta += "、上位%はこの起動中のゲームだけで計算";
  }
  setText(ui.playersMeta, meta);

  placeChildren(ui.players, players.map((player) => {
    let row = playerRows.get(player.nodeId);
    if (!row) {
      row = createPlayerRow(player.nodeId);
      playerRows.set(player.nodeId, row);
    }
    updatePlayerRow(row, player, phase, devices);
    return row;
  }));
}

function createPlayerRow(nodeId) {
  const row = element("tr");
  const node = element("th");
  node.scope = "row";
  node.append(nodeName(nodeId));
  const modes = element("span", "", "seg");
  modes.setAttribute("role", "group");
  modes.setAttribute("aria-label", `${nodeLabel(nodeId)}の操作モード`);
  for (const mode of ["manual", "auto"]) {
    const button = element("button", modeLabel(mode));
    button.type = "button";
    button.dataset.action = "set-mode";
    button.dataset.node = nodeId;
    button.dataset.mode = mode;
    button.setAttribute("aria-label", `${nodeLabel(nodeId)}を${modeLabel(mode)}にする`);
    modes.append(button);
  }
  const modeCell = element("td");
  modeCell.append(modes);
  row.append(node, element("td"), modeCell, element("td"), element("td", "", "num"), element("td", "", "num"),
    element("td"), element("td", "", "num"));
  return row;
}

function updatePlayerRow(row, player, phase, devices) {
  const [, deviceCell, modeCell, readyCell, fifoCell, scoreCell, rankCell, topPercentCell] = row.cells;
  const device = devices.find((d) => d.assignment?.kind === "player" && d.assignment.nodeId === player.nodeId);
  if (!device) {
    setCell(deviceCell, "— なし", "muted");
  } else {
    setCell(deviceCell, device.connected ? "● 接続" : "× 切断", device.connected ? "ok" : "bad");
  }
  for (const button of modeCell.querySelectorAll("button")) {
    setAttr(button, "aria-pressed", String(button.dataset.mode === player.mode));
    button.disabled = phase === "Playing";
  }
  // ready means something only for manual players, and is a start condition
  // only in Preparing, so it is highlighted only there.
  if (player.mode !== "manual") {
    setCell(readyCell, "—", "muted");
  } else if (phase !== "Preparing") {
    setCell(readyCell, player.ready ? "✓ 準備完了" : "! 未準備", "muted");
  } else {
    setCell(readyCell, player.ready ? "✓ 準備完了" : "! 未準備", player.ready ? "ok" : "warn");
  }
  setText(fifoCell, String(player.fifoLength));
  setCell(scoreCell, String(player.score), phase === "Playing" || phase === "Finished" ? "num strong" : "num");
  if (player.rankingEligible === null) {
    setCell(rankCell, "—", "muted");
  } else if (!player.rankingEligible) {
    setCell(rankCell, "対象外", "warn");
  } else if (phase !== "Finished") {
    setCell(rankCell, "対象", "");
  } else if (player.rank === null) {
    setCell(rankCell, "—", "muted");
  } else {
    setCell(rankCell, `${player.rank}位`, player.rank === 1 ? "strong" : "");
  }
  if (phase === "Finished" && player.topPercent !== null) {
    setCell(topPercentCell, `${player.topPercent}%`, "num");
  } else {
    setCell(topPercentCell, "—", "num muted");
  }
}

// ---------- Bot表 ----------

const botRows = new Map();

export function renderBots(snapshot) {
  placeChildren(ui.bots, snapshot.bots.map((bot) => {
    let row = botRows.get(bot.nodeId);
    if (!row) {
      row = createBotRow(bot.nodeId);
      botRows.set(bot.nodeId, row);
    }
    updateBotRow(row, bot);
    return row;
  }));
}

function createBotRow(nodeId) {
  const row = element("tr");
  const node = element("th");
  node.scope = "row";
  node.append(nodeName(nodeId));
  const meter = element("span", "", "meter");
  meter.setAttribute("aria-hidden", "true");
  for (let i = 0; i < BOT_FIFO_MAX; i++) {
    meter.append(element("i"));
  }
  const fifo = element("td");
  fifo.append(meter, element("span"));

  // The rate input is never written by the polls, nor filled with the
  // current rate, so that a typed value survives them (admin-ui.md 5.4).
  const input = element("input");
  input.type = "number";
  input.step = "any";
  input.inputMode = "decimal";
  input.dataset.rateFor = nodeId;
  input.setAttribute("aria-label", `${nodeLabel(nodeId)}の新しい処理速度（pps）`);
  const set = element("button", "設定", "btn");
  set.type = "button";
  set.dataset.action = "set-rate";
  set.dataset.node = nodeId;
  set.setAttribute("aria-label", `${nodeLabel(nodeId)}の処理速度を設定`);
  const controls = element("span", "", "rate");
  controls.append(input, element("span", "pps", "muted"), set);
  const change = element("td", "", "field");
  change.append(controls, fieldError());
  row.append(node, fifo, element("td", "", "num"), change);
  return row;
}

function updateBotRow(row, bot) {
  const [, fifoCell, rateCell] = row.cells;
  const [meter, count] = fifoCell.children;
  [...meter.children].forEach((slot, i) => slot.classList.toggle("on", i < bot.fifoLength));
  setText(count, `${bot.fifoLength} / ${BOT_FIFO_MAX}`);
  setText(rateCell, `${bot.ratePps} pps`);
}

// ---------- 共通 ----------

function nodeName(nodeId) {
  const name = element("span", "", "node");
  name.append(tile(nodeId, displayName(nodeId)), element("span", displayName(nodeId)), element("span", nodeId, "id"));
  return name;
}

function setCell(cell, text, className) {
  setText(cell, text);
  if (cell.className !== className) {
    cell.className = className;
  }
}
