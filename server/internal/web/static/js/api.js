// HTTP client for the admin API (docs/spec/web.md). Every request, including
// reading its body, is aborted after TIMEOUT_MS (web.md 6).
//
// A request resolves to one of:
//   {kind: "success", status, body}
//   {kind: "error", status, code, message, details}
//       the error response of web.md 5
//   {kind: "unknown", reason, status?}
//       no API response, so the server may or may not have processed the
//       request. reason is "timeout", "network", or "bad_response" for a
//       response that is not the API's JSON, or for getState, not a Snapshot.

export const TIMEOUT_MS = 5000;

// getState resolves to a success only when the body is a Snapshot of web.md
// 3.2; any other 2xx body is a bad_response, so that the page reports a
// failed GET instead of throwing while it renders the body.
export async function getState() {
  const result = await request("/api/state", { method: "GET", cache: "no-store" });
  if (result.kind === "success" && !isSnapshot(result.body)) {
    return { kind: "unknown", reason: "bad_response", status: result.status };
  }
  return result;
}

export function postCommand(command) {
  return request("/api/commands", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(command),
  });
}

async function request(url, init) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
  let response, body;
  try {
    response = await fetch(url, { ...init, signal: controller.signal });
    body = await response.json();
  } catch (err) {
    if (controller.signal.aborted) {
      return { kind: "unknown", reason: "timeout" };
    }
    if (err instanceof SyntaxError) {
      return { kind: "unknown", reason: "bad_response", status: response.status };
    }
    return { kind: "unknown", reason: "network" };
  } finally {
    clearTimeout(timer);
  }
  if (response.ok && isObject(body)) {
    return { kind: "success", status: response.status, body };
  }
  const error = body?.error;
  if (!response.ok && body?.ok === false && typeof error?.code === "string" &&
      typeof error.message === "string" && isObject(error.details)) {
    const { code, message, details } = error;
    return { kind: "error", status: response.status, code, message, details };
  }
  return { kind: "unknown", reason: "bad_response", status: response.status };
}

function isObject(v) {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

// isSnapshot checks the types of the fields the page shows (web.md 3.2). It
// does not check the ranges or the number of nodes, which the page shows as
// they are.
function isSnapshot(s) {
  const { game, players, bots, devices, preview, records } = s;
  return isObject(game) && typeof game.phase === "string" &&
    Number.isInteger(game.durationSeconds) && Number.isInteger(game.remainingSeconds) &&
    Number.isInteger(game.countdownSeconds) &&
    typeof game.canStart === "boolean" &&
    isListOf(game.startBlockers, (b) => typeof b.code === "string" &&
      (b.nodeIds === undefined || (Array.isArray(b.nodeIds) && b.nodeIds.every(isString)))) &&
    isListOf(players, (p) => isString(p.nodeId) && isString(p.mode) && typeof p.ready === "boolean" &&
      Number.isInteger(p.fifoLength) && Number.isInteger(p.score) &&
      (p.rankingEligible === null || typeof p.rankingEligible === "boolean") &&
      (p.rank === null || Number.isInteger(p.rank)) &&
      (p.topPercent === null || Number.isInteger(p.topPercent))) &&
    isListOf(bots, (b) => isString(b.nodeId) && Number.isInteger(b.fifoLength) && Number.isFinite(b.ratePps)) &&
    isListOf(devices, (d) => isString(d.deviceId) && isString(d.kind) && typeof d.connected === "boolean" &&
      (d.assignment === null || isNode(d.assignment))) &&
    (preview === null || (isObject(preview) && isString(preview.deviceId) && isString(preview.nodeId))) &&
    isRecords(records);
}

function isRecords(r) {
  return isObject(r) && (r.gameBest === null || isBest(r.gameBest)) &&
    isListOf(r.hourlyBests, (h) => isString(h.hourStart) && isBest(h)) &&
    (r.allTimeBest === null || isBest(r.allTimeBest)) &&
    typeof r.available === "boolean" && (r.unavailableReason === null || isString(r.unavailableReason)) &&
    typeof r.saveFailed === "boolean";
}

function isBest(b) {
  return isObject(b) && Number.isInteger(b.score) && Array.isArray(b.nodeIds) && b.nodeIds.every(isString) &&
    isString(b.finishedAt);
}

function isListOf(v, check) {
  return Array.isArray(v) && v.every((e) => isObject(e) && check(e));
}

function isNode(v) {
  return isObject(v) && isString(v.kind) && isString(v.nodeId);
}

function isString(v) {
  return typeof v === "string";
}
