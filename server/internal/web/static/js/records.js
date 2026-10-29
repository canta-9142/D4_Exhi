// The best scores and the records file (docs/spec/admin-ui.md 5.9, 5.10).
// The bests are shown as app counted them (web.md 3.2); the page neither
// counts nor keeps any of them. The parts are made once and then updated by
// setText and setContent, so that the polls do not rebuild them.

import { chip, element, placeChildren, setContent, setText } from "./dom.js";
import {
  BESTS_NOTE, BESTS_SAVE_FAILED, BESTS_THIS_PROCESS, NO_ELIGIBLE, NO_RECORDS, displayName, finishedLabel, hourLabel,
  jstDate, nodeLabel, recordsUnavailableText,
} from "./labels.js";

const ui = {
  alert: document.getElementById("records-alert"),
  meta: document.getElementById("bests-meta"),
  game: document.getElementById("best-game"),
  allTime: document.getElementById("best-all-time"),
  allTimeNew: document.getElementById("best-all-time-new"),
  hourly: document.getElementById("best-hourly"),
};

export function renderRecords(snapshot) {
  const { game, records } = snapshot;
  renderAlert(records);
  renderMeta(records);

  // gameBest is also null in Finished when no player is ranked; only then
  // does 「この回」 say so (admin-ui.md 5.9).
  const finished = game.phase === "Finished";
  const gameBest = finished ? records.gameBest : null;
  showBest(valueParts(ui.game), gameBest, finished ? NO_ELIGIBLE : "", false);
  showBest(valueParts(ui.allTime), records.allTimeBest, NO_RECORDS, true);
  ui.allTimeNew.hidden = !sameGame(records.allTimeBest, gameBest);
  renderHourly(records.hourlyBests, gameBest);
}

// The alert stays while the records file cannot be used, which lasts until
// the server restarts, so it has no close button (admin-ui.md 5.10).
function renderAlert(records) {
  const unavailable = !records.available;
  ui.alert.hidden = !unavailable;
  document.body.dataset.records = unavailable ? "unavailable" : "available";
  setText(ui.alert, unavailable ? recordsUnavailableText(records.unavailableReason) : "");
}

function renderMeta(records) {
  let text = BESTS_NOTE, className = "meta";
  if (!records.available) {
    text = BESTS_THIS_PROCESS;
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

// sameGame tells whether best is from the game whose best is gameBest.
// Games finish at least 3 seconds apart, so different games never share
// finishedAt (admin-ui.md 14).
function sameGame(best, gameBest) {
  return best !== null && gameBest !== null && best.score === gameBest.score &&
    best.finishedAt === gameBest.finishedAt && best.nodeIds.join() === gameBest.nodeIds.join();
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

function renderHourly(hourly, gameBest) {
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
    updateHourRow(row, hour, i === 0, jstDate(hour.hourStart) !== latestDate, sameGame(hour, gameBest));
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
