// The best scores and the records file (docs/spec/admin-ui.md 5.9, 5.10, 15.4).
// The bests are shown as app counted them (web.md 3.2); the page neither
// counts nor keeps any of them. The parts are made once and then updated by
// setText and setContent, so that the polls do not rebuild them.

import { chip, element, placeChildren, setContent, setText } from "./dom.js";
import {
  BESTS_NOTE, BESTS_SAVE_FAILED, BESTS_THIS_PROCESS, NO_ELIGIBLE, NO_RECORDS, NO_ROUNDS, ROUNDS_NOTE,
  ROUNDS_THIS_PROCESS, displayName, finishedLabel, hourLabel, isSimultaneous, jstDate, nodeLabel,
  recordsUnavailableText,
} from "./labels.js";

const ui = {
  alert: document.getElementById("records-alert"),
  meta: document.getElementById("bests-meta"),
  recent: document.getElementById("best-recent"),
  game: document.getElementById("best-game"),
  allTime: document.getElementById("best-all-time"),
  allTimeNew: document.getElementById("best-all-time-new"),
  hourly: document.getElementById("best-hourly"),
};

export function renderRecords(snapshot) {
  const { game, records } = snapshot;
  const simultaneous = isSimultaneous(game);
  renderAlert(records);
  renderMeta(records, simultaneous);

  // latest is the result whose NEW mark may be shown: the simultaneous game's
  // 「この回」, or the newest of the recent rounds in endless operation.
  let latest;
  if (simultaneous) {
    // gameBest is also null in Finished when no player is ranked; only then
    // does 「この回」 say so (admin-ui.md 15.4).
    const finished = game.phase === "Finished";
    latest = finished ? records.gameBest : null;
    showBest(valueParts(ui.game), latest, finished ? NO_ELIGIBLE : "", false);
  } else {
    renderRecent(records.recentRounds);
    const round = records.recentRounds[0];
    latest = round ? { score: round.score, nodeIds: [round.nodeId], finishedAt: round.finishedAt } : null;
  }
  showBest(valueParts(ui.allTime), records.allTimeBest, NO_RECORDS, true);
  ui.allTimeNew.hidden = !sameGame(records.allTimeBest, latest);
  renderHourly(records.hourlyBests, latest);
}

// The alert stays while the records file cannot be used, which lasts until
// the server restarts, so it has no close button (admin-ui.md 5.10).
function renderAlert(records) {
  const unavailable = !records.available;
  ui.alert.hidden = !unavailable;
  document.body.dataset.records = unavailable ? "unavailable" : "available";
  setText(ui.alert, unavailable ? recordsUnavailableText(records.unavailableReason) : "");
}

function renderMeta(records, simultaneous) {
  let text = simultaneous ? BESTS_NOTE : ROUNDS_NOTE, className = "meta";
  if (!records.available) {
    text = simultaneous ? BESTS_THIS_PROCESS : ROUNDS_THIS_PROCESS;
    className = "meta warn";
  } else if (records.saveFailed) {
    // The result field loses the failure at the next operation, so the
    // panel keeps saying it while it lasts.
    text = BESTS_SAVE_FAILED;
    className = "meta bad";
  }
  setText(ui.meta, text);
  if (ui.meta.className !== className) {
    ui.meta.className = className;
  }
}

// sameGame tells whether best is from the game or the round whose result is
// latest. Games finish at least 3 seconds apart, so different games never
// share finishedAt (admin-ui.md 14). A round that ties an earlier one is not
// the one counted, so it matches nothing.
function sameGame(best, latest) {
  return best !== null && latest !== null && best.score === latest.score &&
    best.finishedAt === latest.finishedAt && best.nodeIds.join() === latest.nodeIds.join();
}

// ---------- 直近のラウンド ----------

// The rows are keyed by finishedAt and nodeId: a new round adds a row at the
// top, the others are updated cell by cell, and a round beyond the newest ten
// loses its row (admin-ui.md 12).
const recentRows = new Map();
let recentEmptyRow = null;

function renderRecent(rounds) {
  if (rounds.length === 0) {
    recentEmptyRow ??= placeholderRow(NO_ROUNDS);
    placeChildren(ui.recent, [recentEmptyRow]);
    recentRows.clear();
    return;
  }
  const seen = new Map();
  const live = new Set();
  placeChildren(ui.recent, rounds.map((round, i) => {
    // Rounds never share both, but a repeat must not share a row.
    const base = `${round.finishedAt}|${round.nodeId}`;
    const repeat = seen.get(base) ?? 0;
    seen.set(base, repeat + 1);
    const key = repeat === 0 ? base : `${base}#${repeat}`;
    live.add(key);
    let row = recentRows.get(key);
    if (!row) {
      row = createRecentRow();
      recentRows.set(key, row);
    }
    updateRecentRow(row, round, i === 0);
    return row;
  }));
  for (const key of recentRows.keys()) {
    if (!live.has(key)) {
      recentRows.delete(key);
    }
  }
}

function createRecentRow() {
  const row = element("tr");
  const at = element("th", "", "best-at");
  at.scope = "row";
  row.append(at, element("td", "", "best-nodes"), element("td", "", "num best-score"), element("td", "", "num best-top"));
  return row;
}

function updateRecentRow(row, round, latest) {
  const [at, node, score, top] = row.cells;
  setText(at, finishedLabel(round.finishedAt, false));
  setContent(node, round.nodeId, () => [chip(round.nodeId, displayName(round.nodeId), nodeLabel(round.nodeId))]);
  setText(score, String(round.score));
  setText(top, `上位 ${round.topPercent}%`);
  row.classList.toggle("latest", latest);
}

// showBest writes a best, or "—" and empty, into parts {score, nodes, at}.
function showBest(parts, best, empty, withDate) {
  setText(parts.score, best ? String(best.score) : "—");
  if (best) {
    setContent(parts.nodes, `nodes:${best.nodeIds.join()}`,
      () => best.nodeIds.map((nodeId) => chip(nodeId, displayName(nodeId), nodeLabel(nodeId))));
  } else {
    setContent(parts.nodes, `empty:${empty}`, () => (empty ? [element("span", empty, "muted")] : []));
  }
  setText(parts.at, best ? finishedLabel(best.finishedAt, withDate) : "");
}

// valueParts makes the parts of 「この回」 or 「通算」 in place of the
// placeholder the first time.
const values = new WeakMap();

function valueParts(container) {
  let parts = values.get(container);
  if (!parts) {
    parts = {
      score: element("span", "", "best-score"),
      nodes: element("span", "", "best-nodes"),
      at: element("span", "", "best-at"),
    };
    container.replaceChildren(parts.score, parts.nodes, parts.at);
    values.set(container, parts);
  }
  return parts;
}

// ---------- 1時間ごと ----------

// The rows are keyed by hourStart: a new hour adds a row, and the others
// are updated cell by cell.
const hourRows = new Map();
let emptyRow = null;

function renderHourly(hourly, latest) {
  if (hourly.length === 0) {
    emptyRow ??= placeholderRow(NO_RECORDS);
    placeChildren(ui.hourly, [emptyRow]);
    hourRows.clear();
    return;
  }
  // Only hours of another date than the latest one carry the date
  // (admin-ui.md 7.4).
  const latestDate = jstDate(hourly[0].hourStart);
  placeChildren(ui.hourly, hourly.map((hour, i) => {
    let row = hourRows.get(hour.hourStart);
    if (!row) {
      row = createHourRow();
      hourRows.set(hour.hourStart, row);
    }
    updateHourRow(row, hour, i === 0, jstDate(hour.hourStart) !== latestDate, sameGame(hour, latest));
    return row;
  }));
  for (const key of hourRows.keys()) {
    if (!hourly.some((hour) => hour.hourStart === key)) {
      hourRows.delete(key);
    }
  }
}

function createHourRow() {
  const row = element("tr");
  const head = element("th");
  head.scope = "row";
  const mark = element("span", "NEW", "new");
  mark.hidden = true;
  head.append(element("span"), mark);
  row.append(head, element("td", "", "num best-score"), element("td", "", "best-nodes"), element("td", "", "best-at"));
  return row;
}

function updateHourRow(row, hour, latest, withDate, isNew) {
  const [head, score, nodes, at] = row.cells;
  const [label, mark] = head.children;
  setText(label, hourLabel(hour.hourStart, withDate));
  mark.hidden = !isNew;
  row.classList.toggle("latest", latest);
  showBest({ score, nodes, at }, hour, "", false);
}

function placeholderRow(text) {
  const row = element("tr");
  const cell = element("td", text, "placeholder");
  cell.colSpan = 4;
  row.append(cell);
  return row;
}
