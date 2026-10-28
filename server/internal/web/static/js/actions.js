// The admin operations (docs/spec/admin-ui.md 6). Clicks and Enter on the
// controls inside the fieldset are turned into the commands of web.md 4.1,
// confirmed first where the spec says so, and sent only through runCommand.
// The page checks only what it needs to make a command; whether app accepts
// it is left to app.

import { confirm } from "./dialogs.js";
import { RATE_NOT_A_NUMBER, choosePrompts, commandPhrases } from "./labels.js";

let deps = null;

// install listens on root. runCommand(command, phrases) sends a command and
// resolves to its api.js result, or null when nothing was sent;
// getSnapshot() returns the snapshot on screen; getSelection() and
// select(deviceId) read and change the selected device.
export function install(root, dependencies) {
  deps = dependencies;
  root.addEventListener("click", (event) => {
    const control = event.target.closest("button[data-action]");
    if (control && root.contains(control) && !control.matches(":disabled")) {
      act(control);
    }
  });
  root.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && event.target.matches("input[data-rate-for]") && !event.isComposing) {
      event.preventDefault();
      const set = event.target.closest("tr").querySelector("button[data-action=set-rate]");
      if (!set.matches(":disabled")) {
        act(set);
      }
    }
  });
  // A message about an empty input or select goes once it is changed.
  for (const type of ["input", "change"]) {
    root.addEventListener(type, (event) => {
      if (event.target.matches("input[data-rate-for], select[data-choice]")) {
        showError(event.target, "");
      }
    });
  }
}

async function act(control) {
  const snapshot = deps.getSnapshot();
  if (!snapshot) {
    return;
  }
  const action = control.dataset.action;
  switch (action) {
    case "start":
      return send({ type: "start", data: {} }, snapshot);
    case "finish":
    case "reset":
      if (await confirm(action, {}, snapshot, control)) {
        await send({ type: action, data: {} }, snapshot);
      }
      return;
    case "set-mode":
      // The pressed button is the current mode: pressing it sends nothing.
      if (control.getAttribute("aria-pressed") !== "true") {
        await send({ type: "set_node_mode", data: { nodeId: control.dataset.node, mode: control.dataset.mode } }, snapshot);
      }
      return;
    case "set-rate":
      return setRate(control, snapshot);
    case "preview-stop":
      return send({ type: "set_device_preview", data: { deviceId: null } }, snapshot);
    case "deselect":
      return deps.select(null);
    default:
      return deviceAction(action, control, snapshot);
  }
}

// setRate sends the typed rate as Number() reads it. An empty input or one
// that is not a finite number sends nothing; zero and negative numbers are
// sent, and app's rejection tells why (admin-ui.md 5.4).
async function setRate(control, snapshot) {
  const input = control.closest("tr").querySelector("input[data-rate-for]");
  const text = input.value.trim();
  const rate = Number(text);
  if (text === "" || !Number.isFinite(rate)) {
    showError(input, RATE_NOT_A_NUMBER);
    return;
  }
  const result = await send({ type: "set_bot_rate", data: { nodeId: control.dataset.node, ratePps: rate } }, snapshot);
  if (result?.kind === "success") {
    input.value = "";
  }
}

async function deviceAction(action, control, snapshot) {
  const deviceId = deps.getSelection();
  const device = snapshot.devices.find((d) => d.deviceId === deviceId);
  if (!device) {
    return;
  }
  if (action === "preview-start") {
    return send({ type: "set_device_preview", data: { deviceId } }, snapshot);
  }
  if (action === "unassign") {
    return send({ type: "set_device_assignment", data: { deviceId, target: null } }, snapshot);
  }
  const select = control.closest(".op")?.querySelector("select[data-choice]");
  if (!select) {
    return;
  }
  const chosen = select.value;
  if (chosen === "") {
    showError(select, choosePrompts[select.dataset.choice]);
    return;
  }
  let command;
  if (action === "assign") {
    command = { type: "set_device_assignment", data: { deviceId, target: { kind: device.kind, nodeId: chosen } } };
  } else if (action === "swap") {
    command = { type: "swap_device_assignments", data: { firstDeviceId: deviceId, secondDeviceId: chosen } };
    if (!await confirm("swap", { first: deviceId, second: chosen }, snapshot, control)) {
      return;
    }
  } else if (action === "replace") {
    // The assigned one of the two hands its target over (admin-ui.md 8.2).
    const [oldDeviceId, newDeviceId] = device.assignment ? [deviceId, chosen] : [chosen, deviceId];
    command = { type: "replace_device_assignment", data: { oldDeviceId, newDeviceId } };
    if (!await confirm("replace", { oldDeviceId, newDeviceId }, snapshot, control)) {
      return;
    }
  } else {
    return;
  }
  const result = await send(command, snapshot);
  // On a rejection or an unknown result the choice stays for a retry.
  if (result?.kind === "success") {
    select.value = "";
  }
}

// send runs command with the wording made from the snapshot it was made
// from, which a confirmation may have kept on screen for a while.
function send(command, snapshot) {
  return deps.runCommand(command, commandPhrases(command, snapshot.devices));
}

function showError(control, text) {
  const error = control.closest(".field, .op")?.querySelector(".field-error");
  if (error && error.textContent !== text) {
    error.textContent = text;
  }
}
