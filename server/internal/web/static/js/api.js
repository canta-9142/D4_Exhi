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
//       response that is not the API's JSON.

export const TIMEOUT_MS = 5000;

export function getState() {
  return request("/api/state", { method: "GET", cache: "no-store" });
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
