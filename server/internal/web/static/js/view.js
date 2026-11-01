// The command bars, the player table and the Bot table (docs/spec/admin-ui.md
// 5.2-5.4, 15.2, 15.3). The screen of endless operation is the default; the
// one of the simultaneous game is shown when game.mode says so. The table
// rows are made once and then updated cell by cell, so that the polls keep the
// focus and a typed Bot rate. The remaining time, the elapsed time and the
// scores are shown as fetched, without counting down or adding points
// (web.md 6).

import { chip, element, fieldError, placeChildren, setAttr, setContent, setText, tile } from "./dom.js";
import {
  BOT_FIFO_MAX, COUNTDOWN_LABEL, FINISHED_HINT, NO_MANUAL_HINT, NO_MANUAL_RUNNING_HINT, OPERATIONS, PHASES,
  REMAINING_LABEL, RUNNING_HINT, TOP_THIS_PROCESS_GAMES, TOP_THIS_PROCESS_ROUNDS, blockerText, canReturnToBot,
  displayName, formatElapsed, isSimultaneous, modeLabel, nodeLabel, operationLabel, phaseLabel, roundCounts,
  roundCountsText, stationText,
} from "./labels.js";

const ui = {
  endlessBar: document.getElementById("command-endless"),
  operation: document.getElementById("operation"),
  operationTrack: document.getElementById("operation-track"),
  elapsed: document.getElementById("elapsed"),
  roundCounts: document.getElementById("round-counts"),
  endlessReadiness: document.getElementById("e-readiness"),
  endlessBlockers: document.getElementById("e-blockers"),
  endlessHint: document.getElementById("e-hint"),
  endlessStart: document.getElementById("e-start"),
  endlessStop: document.getElementById("e-stop"),

  simultaneousBar: document.getElementById("command-simultaneous"),
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
  allManual: document.getElementById("all-manual"),
  allAuto: document.getElementById("all-auto"),
  rankHead: document.getElementById("rank-head"),
  players: document.getElementById("players"),
  bots: document.getElementById("bots"),
};

const leadHeads = document.querySelectorAll("th[data-lead]");

export function renderGame(snapshot) {
  const { game } = snapshot;
  const simultaneous = isSimultaneous(game);
  // The rest of the page follows these: the columns, the best scores and the
  // style of each screen (data-for in index.html).
  setAttr(document.body, "data-mode", simultaneous ? "simultaneous" : "endless");
  setAttr(document.body, "data-operation", game.operation);
  setAttr(document.body, "data-phase", game.phase);
  ui.endlessBar.hidden = simultaneous;
  ui.simultaneousBar.hidden = !simultaneous;
  if (simultaneous) {
    renderSimultaneousBar(snapshot);
  } else {
    renderEndlessBar(snapshot);
  }
}

// ---------- 連続運転のコマンドバー（admin-ui.md 5.2） ----------

function renderEndlessBar({ game, players }) {
  const running = game.operation === "running";
  setText(ui.operation, operationLabel(game.operation));
  for (const step of ui.operationTrack.children) {
    setAttr(step, "aria-current", step.dataset.operation === game.operation ? "step" : null);
  }
  // Each station has its own clock, so there is no remaining time here; the
  // elapsed time and the players in a round are shown while it runs.
  setText(ui.elapsed, running ? formatElapsed(game.elapsedMs) : "—");
  setText(ui.roundCounts, running ? roundCountsText(roundCounts(players)) : "—");

  // While it runs, not being able to start is the normal state, so a lone
  // not_stopped is not shown as an alert.
  const quiet = !game.canStart && game.startBlockers.every((b) => b.code === "not_stopped");
  ui.endlessReadiness.dataset.canStart = quiet ? "quiet" : String(game.canStart);
  setText(ui.endlessReadiness, game.canStart ? "✓ 運転を開始できます" : "開始できない理由");
  setContent(ui.endlessBlockers, JSON.stringify(game.startBlockers), () => game.startBlockers.map(blockerItem));
  let hint = "";
  if (game.canStart && !players.some((p) => p.mode === "manual")) {
    hint = NO_MANUAL_RUNNING_HINT;
  } else if (running) {
    hint = RUNNING_HINT;
  }
  setText(ui.endlessHint, hint);
  ui.endlessHint.hidden = hint === "";

  // Only canStart and the operation decide what can be pressed (admin-ui.md
  // 8.3). An operation the page does not know leaves stopping to app.
  ui.endlessStart.disabled = !game.canStart;
  ui.endlessStop.disabled = OPERATIONS.includes(game.operation) && !running;
}

// ---------- 一斉ゲーム方式のコマンドバー（admin-ui.md 15.2） ----------

function renderSimultaneousBar({ game, players }) {
  setText(ui.phase, phaseLabel(game.phase));
  for (const step of ui.track.children) {
    setAttr(step, "aria-current", step.dataset.phase === game.phase ? "step" : null);
  }
  // The countdown before the game time takes the place of the remaining time;
  // the phase stays Playing (admin-ui.md 15.2).
  const countdown = game.countdownSeconds > 0;
  document.body.dataset.countdown = String(countdown);
  setText(ui.timeLabel, countdown ? COUNTDOWN_LABEL : REMAINING_LABEL);
  setText(ui.remaining, String(countdown ? game.countdownSeconds : game.remainingSeconds));
  setText(ui.duration, countdown ? "" : `/ ${game.durationSeconds} 秒`);
  const ratio = game.durationSeconds > 0 ? Math.min(Math.max(game.remainingSeconds / game.durationSeconds, 0), 1) : 0;
  ui.timeBar.style.width = `${ratio * 100}%`;

  // Outside Preparing, not being able to start is the normal state, so a
  // lone not_preparing is not shown as an alert (admin-ui.md 15.2).
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

  // Only canStart and the phase decide what can be pressed (admin-ui.md 15.5).
  ui.start.disabled = !game.canStart;
  // An unknown phase disables nothing and leaves the decision to app.
  ui.finish.disabled = PHASES.includes(game.phase) && game.phase !== "Playing";

  // After a game the reset takes the place of the start button, and the small
  // one goes, so that the same operation is not shown twice (admin-ui.md 15.2).
  const finished = game.phase === "Finished";
  ui.start.hidden = finished;
  ui.resetNext.hidden = !finished;
  ui.reset.classList.toggle("away", finished);
}

function blockerItem(blocker) {
  const quiet = blocker.code === "not_preparing" || blocker.code === "not_stopped";
  const item = element("li", "", quiet ? "quiet" : "");
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
  const simultaneous = isSimultaneous(game);
  const phase = game.phase;
  for (const head of leadHeads) {
    head.classList.toggle("lead", simultaneous && head.dataset.lead === phase);
  }
  setText(ui.rankHead, simultaneous && phase === "Finished" ? "順位" : "ランキング");
  setText(ui.playersMeta, playersMeta(snapshot));
  // The bulk switches are accepted in the same phases as the row buttons
  // (admin-ui.md 8.3, 15.5); endless operation accepts them always.
  ui.allManual.disabled = ui.allAuto.disabled = simultaneous && phase === "Playing";

  placeChildren(ui.players, players.map((player) => {
    let row = playerRows.get(player.nodeId);
    if (!row) {
      row = createPlayerRow(player.nodeId);
      playerRows.set(player.nodeId, row);
    }
    updatePlayerRow(row, player, snapshot);
    return row;
  }));
}

// playersMeta is the line beside the heading of the player table
// (admin-ui.md 5.3, 15.3).
function playersMeta({ game, players, records }) {
  const manual = players.filter((p) => p.mode === "manual");
  // The percentages then count only the rounds or games of this run, as the
  // best scores do.
  const hasPercent = players.some((p) => p.topPercent !== null);
  if (isSimultaneous(game)) {
    let meta = `手動 ${manual.length} 人`;
    if (game.phase === "Preparing") {
      meta += `、うち準備完了 ${manual.filter((p) => p.ready).length} 人`;
    } else if (game.phase === "Playing") {
      meta = "ゲーム中は操作モードを変更できません";
    } else if (game.phase === "Finished" && !records.available && hasPercent) {
      meta += `、${TOP_THIS_PROCESS_GAMES}`;
    }
    return meta;
  }
  let meta = game.operation === "stopped"
    ? `停止中。手動 ${manual.length} 人`
    : `手動 ${manual.length} 人、ラウンド中 ${players.filter((p) => p.station === "playing").length} 人`;
  if (!records.available && players.some((p) => p.station === "finished" && p.topPercent !== null)) {
    meta += `、${TOP_THIS_PROCESS_ROUNDS}`;
  }
  return meta;
}

// Cells that only one screen shows carry data-for, which style.css hides on
// the other screen.
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
  // The label is short for the width of the column; the dialog and the
  // aria-label say 「ボットに戻す」 (admin-ui.md 5.3).
  const back = element("button", "戻す…", "btn back");
  back.type = "button";
  back.dataset.action = "return-to-bot";
  back.dataset.node = nodeId;
  back.dataset.for = "endless";
  back.setAttribute("aria-label", `${nodeLabel(nodeId)}をボットに戻す`);
  const modeCell = element("td", "", "opcell");
  modeCell.append(modes, back);

  const state = element("td");
  state.dataset.for = "endless";
  const ready = element("td");
  ready.dataset.for = "simultaneous";
  const rank = element("td");
  rank.dataset.for = "simultaneous";
  row.append(node, element("td"), modeCell, state, ready, element("td", "", "num"), element("td", "", "num"), rank,
    element("td", "", "num"));
  return row;
}

function updatePlayerRow(row, player, { game, devices }) {
  const [, deviceCell, modeCell, stateCell, readyCell, fifoCell, scoreCell, rankCell, topPercentCell] = row.cells;
  const simultaneous = isSimultaneous(game);
  const phase = game.phase;
  const device = devices.find((d) => d.assignment?.kind === "player" && d.assignment.nodeId === player.nodeId);
  if (!device) {
    setCell(deviceCell, "— なし", "muted");
  } else {
    setCell(deviceCell, device.connected ? "● 接続" : "× 切断", device.connected ? "ok" : "bad");
  }
  for (const button of modeCell.querySelectorAll(".seg button")) {
    setAttr(button, "aria-pressed", String(button.dataset.mode === player.mode));
    button.disabled = simultaneous && phase === "Playing";
  }
  modeCell.querySelector(".back").disabled = !canReturnToBot(game.operation, player.station);
  setText(fifoCell, String(player.fifoLength));

  if (simultaneous) {
    updateSimultaneousCells(player, phase, readyCell, scoreCell, rankCell, topPercentCell);
    return;
  }
  const { text, tone } = stationText(player);
  setCell(stateCell, text, tone === "" ? "strong" : tone);
  // The score of a normal round counts while it is played and stays in the
  // result; a practice has none (admin-ui.md 5.3).
  const scored = player.station === "finished" || (player.station === "playing" && player.round === "normal");
  setCell(scoreCell, scored ? String(player.score) : "—", scored ? "num strong" : "num muted");
  if (player.station === "finished" && player.topPercent !== null) {
    setCell(topPercentCell, `${player.topPercent}%`, "num");
  } else {
    setCell(topPercentCell, "—", "num muted");
  }
}

const roundNames = { normal: "通常", tutorial: "練習" };

function updateSimultaneousCells(player, phase, readyCell, scoreCell, rankCell, topPercentCell) {
  // ready means something only for manual players, and is a start condition
  // only in Preparing, so it is highlighted only there (admin-ui.md 15.3).
  if (player.mode !== "manual") {
    setCell(readyCell, "—", "muted");
  } else {
    let text = "! 未準備", tone = "warn";
    if (player.ready) {
      const round = roundNames[player.round];
      text = round ? `✓ 準備完了 ${round}` : "✓ 準備完了";
      tone = "ok";
    } else if (player.station === "select") {
      text = "! 選択中";
    }
    setCell(readyCell, text, phase === "Preparing" ? tone : "muted");
  }
  if (player.round === "tutorial") {
    setCell(scoreCell, "練習", "num");
  } else {
    setCell(scoreCell, String(player.score), phase === "Playing" || phase === "Finished" ? "num strong" : "num");
  }
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
