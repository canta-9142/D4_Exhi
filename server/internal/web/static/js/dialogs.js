// The confirmation dialog (docs/spec/admin-ui.md 5.8, 11.6). There is one
// <dialog>, inside the fieldset, so that the disabled fieldset also disables
// its confirm button. What is sent is fixed by the caller before the dialog
// opens; while it is open, only the shown values follow the polls.

import { chip, element, setContent, setText } from "./dom.js";
import { dialogText, displayName, targetLabel } from "./labels.js";

const ui = {
  dialog: document.getElementById("confirm-dialog"),
  title: document.getElementById("dialog-title"),
  table: document.getElementById("dialog-table"),
  after: document.getElementById("dialog-after"),
  rows: document.getElementById("dialog-rows"),
  body: document.getElementById("dialog-body"),
  cancel: document.getElementById("dialog-cancel"),
  confirm: document.getElementById("dialog-confirm"),
};

// open is the dialog being shown: {kind, context, opener, resolve}.
let open = null;

ui.cancel.addEventListener("click", () => ui.dialog.close("cancel"));
ui.confirm.addEventListener("click", () => ui.dialog.close("confirm"));
// Esc closes the dialog as a cancel, without a returnValue.
ui.dialog.addEventListener("close", () => {
  const closed = open;
  open = null;
  if (!closed) {
    return;
  }
  const confirmed = ui.dialog.returnValue === "confirm";
  if (!confirmed) {
    closed.opener?.focus();
  }
  closed.resolve(confirmed);
});

// confirm opens the dialog of kind ("finish", "reset", "all-auto", "swap" or
// "replace")
// and resolves to whether it was confirmed. context holds the Device IDs of
// swap and replace; opener gets the focus back on a cancel.
export function confirm(kind, context, snapshot, opener) {
  if (open) {
    return Promise.resolve(false);
  }
  return new Promise((resolve) => {
    open = { kind, context, opener, resolve };
    ui.dialog.returnValue = "";
    show(snapshot);
    ui.dialog.showModal();
    ui.cancel.focus();
  });
}

// updateDialog follows the latest snapshot while the dialog is open.
export function updateDialog(snapshot) {
  if (open) {
    show(snapshot);
  }
}

function show(snapshot) {
  const text = dialogText(open.kind, open.context, snapshot);
  setText(ui.title, text.title);
  setText(ui.confirm, text.confirm);
  ui.confirm.classList.toggle("danger", text.danger);
  ui.table.hidden = !text.rows;
  if (text.rows) {
    setText(ui.after, text.after);
    syncChildren(ui.rows, text.rows.length, () => {
      const row = element("tr");
      row.append(element("td", "", "dev-id"), element("td"), element("td", "→", "muted"), element("td"));
      return row;
    });
    text.rows.forEach((r, i) => {
      const [id, now, , after] = ui.rows.children[i].cells;
      setText(id, r.device && !r.device.connected ? `${r.deviceId}（切断中）` : r.deviceId);
      setTarget(now, r.device ? r.now : undefined, false);
      setTarget(after, r.device ? r.after : undefined, true);
    });
  }
  syncChildren(ui.body, text.paragraphs.length, () => element("p"));
  text.paragraphs.forEach((paragraph, i) => setText(ui.body.children[i], paragraph));
}

// setTarget shows an assignment, null for none, or undefined for a device
// that is no longer listed.
function setTarget(cell, target, strong) {
  const key = target === undefined ? "gone" : target ? `${target.kind}:${target.nodeId}` : "none";
  setContent(cell, `${key}:${strong}`, () => {
    if (target === undefined) {
      return [element("span", "一覧にありません", "muted")];
    }
    if (target === null) {
      return [chip(null, "", "未割当", "empty", strong ? "strong" : "")];
    }
    return [chip(target.nodeId, displayName(target.nodeId), targetLabel(target), target.kind === "led" ? "led" : "",
      strong ? "strong" : "")];
  });
}

function syncChildren(parent, count, create) {
  while (parent.children.length < count) {
    parent.append(create());
  }
  while (parent.children.length > count) {
    parent.lastElementChild.remove();
  }
}
