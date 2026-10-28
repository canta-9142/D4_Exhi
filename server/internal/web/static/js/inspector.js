// The panel of the selected device (docs/spec/admin-ui.md 5.7). It is made
// again when another device is selected, or when the selected one changes
// between assigned and unassigned, which have different operations; other
// changes only update its values, so that a chosen second device survives
// the polls. The candidates of a select are narrowed by the device kind and
// by whether an assignment is needed for the operation to mean anything;
// whether app accepts it is left to app.

import { chip, element, fieldError, setContent, setOptions, setText } from "./dom.js";
import {
  ASSIGNED_HINT, INSPECTOR_EMPTY, TARGETS, UNASSIGNED_HINT, displayName, inspectorPreviewNote, kindLabel,
  missingSelectionText, nodeLabel, targetLabel,
} from "./labels.js";

const ui = {
  meta: document.getElementById("inspector-meta"),
  body: document.getElementById("inspector"),
};

// shown is the key of what the panel is made for.
let shown = "empty";
let parts = null;

export function renderInspector(snapshot, deviceId, missing) {
  const phase = snapshot.game.phase;
  setText(ui.meta, phase === "Playing" ? "ゲーム中は割当を変更できません" : "");
  const device = snapshot.devices.find((d) => d.deviceId === deviceId);
  if (!device) {
    // A missing selection is reported until another device is selected.
    if (missing !== null) {
      show(`missing:${missing}`, () => [element("p", missingSelectionText(missing), "insp-empty warn")]);
    } else {
      show("empty", () => [element("p", INSPECTOR_EMPTY, "insp-empty")]);
    }
    parts = null;
    return;
  }
  const assigned = Boolean(device.assignment);
  show(`device:${device.deviceId}:${device.kind}:${assigned}`, () => {
    parts = build(device, assigned);
    return parts.children;
  });
  update(parts, device, snapshot);
}

function show(key, build) {
  if (shown !== key) {
    shown = key;
    ui.body.replaceChildren(...build());
  }
}

function build(device, assigned) {
  const id = element("div", "", "insp-id");
  const deselect = button("選択を解除", "deselect", `${device.deviceId} の選択を解除`);
  id.append(element("span", device.deviceId, "dev-id"), deselect);
  const facts = element("div", "", "insp-facts");
  facts.append(element("span", kindLabel(device.kind)), element("span"), element("span"));
  const note = element("p", "", "insp-note");
  note.hidden = true;

  const preview = button("確認表示を始める", "preview-start");
  const ops = element("div", "", "ops");
  const p = { preview, facts, note, selects: {}, phaseButtons: [] };
  const buttons = element("div", "", "op-buttons");
  buttons.append(preview);
  if (assigned) {
    const unassign = button("割当を外す", "unassign", `${device.deviceId} の割当を外す`);
    buttons.append(unassign);
    p.phaseButtons.push(unassign);
  }
  ops.append(buttons);
  // The first option says what to choose, so the label is only for screen
  // readers.
  const op = (choice, label, first, text, action, target) => {
    const select = element("select");
    select.dataset.choice = choice;
    select.setAttribute("aria-label", label);
    setOptions(select, [{ value: "", text: first }]);
    const go = button(text, action, `${device.deviceId} ${target}`);
    const row = element("div", "", "op-row");
    row.append(select, go);
    const box = element("div", "", "op");
    box.append(row, fieldError());
    ops.append(box);
    p.selects[choice] = { select, first };
    p.phaseButtons.push(select, go);
  };
  if (assigned) {
    op("target", "割当を変える先", "空いている割当先を選ぶ", "割当を変える", "assign", "の割当を変える");
    op("swap", "入れ替える相手", "入れ替える相手を選ぶ", "入れ替える…", "swap", "を入れ替える");
    op("replace", "交代する相手", "交代する相手を選ぶ", "交代する…", "replace", "を交代する");
  } else {
    op("target", "割り当てる先", "空いている割当先を選ぶ", "割り当てる", "assign", "を割り当てる");
    op("replace", "交代する相手", "交代する相手を選ぶ", "交代する…", "replace", "を交代する");
  }
  ops.append(element("p", assigned ? ASSIGNED_HINT : UNASSIGNED_HINT, "op-hint"));
  p.children = [id, facts, note, ops];
  return p;
}

function update(p, device, snapshot) {
  const { devices, preview, game } = snapshot;
  const [, connected, assignment] = p.facts.children;
  setText(connected, device.connected ? "● 接続" : "× 切断");
  connected.className = device.connected ? "ok" : "bad";
  const a = device.assignment;
  setContent(assignment, a ? `${a.kind}:${a.nodeId}` : "none", () => a
    ? [element("span", "割当"), chip(a.nodeId, displayName(a.nodeId), targetLabel(a), a.kind === "led" ? "led" : "")]
    : [element("span", "未割当", "warn")]);

  const previewed = preview?.deviceId === device.deviceId;
  p.note.hidden = !previewed;
  setText(p.note, previewed ? inspectorPreviewNote(device, preview) : "");
  // Stopping a preview is accepted in every phase (server.md 4.2).
  setText(p.preview, previewed ? "確認表示を止める" : "確認表示を始める");
  p.preview.dataset.action = previewed ? "preview-stop" : "preview-start";
  p.preview.setAttribute("aria-label", `${device.deviceId} の${previewed ? "確認表示を止める" : "確認表示を始める"}`);
  p.preview.disabled = !previewed && game.phase === "Playing";
  for (const control of p.phaseButtons) {
    control.disabled = game.phase === "Playing";
  }

  const others = devices.filter((d) => d.kind === device.kind && d.deviceId !== device.deviceId);
  const targets = TARGETS[device.kind] ?? [];
  const userOf = (nodeId) => others.find((d) => d.assignment?.nodeId === nodeId);
  const assignedOthers = targets.map(userOf).filter(Boolean);
  const options = (choice, list) => setOptions(p.selects[choice].select,
    [{ value: "", text: p.selects[choice].first }, ...list]);

  options("target", targets.map((nodeId) => {
    const name = device.kind === "led" ? `LED ${nodeId}` : nodeLabel(nodeId);
    const user = userOf(nodeId);
    let text = `${name} 空き`;
    if (a?.nodeId === nodeId) {
      text = `${name}（現在の割当）`;
    } else if (user) {
      text = `${name} 使用中 ${deviceText(user)}`;
    }
    return { value: nodeId, text };
  }));
  const byTarget = (d) => ({ value: d.deviceId, text: `${targetLabel(d.assignment)}${GAP}${deviceText(d)}` });
  if (a) {
    options("swap", assignedOthers.map(byTarget));
    // A spare, unassigned and connected, is the usual choice, so spares
    // come first.
    const spare = (d) => !d.assignment && d.connected;
    options("replace", [...others.filter(spare), ...others.filter((d) => !spare(d))].map((d) => ({
      value: d.deviceId,
      text: `${deviceText(d)}${GAP}${d.assignment ? `現在 ${targetLabel(d.assignment)}` : "未割当"}`,
    })));
  } else {
    options("replace", assignedOthers.map(byTarget));
  }
}

// An option collapses spaces, so the gap between its parts is made of
// no-break spaces.
const GAP = "  ";

function deviceText(device) {
  return device.connected ? device.deviceId : `${device.deviceId}（切断中）`;
}

// button names its target in aria-label (admin-ui.md 12).
function button(text, action, label) {
  const e = element("button", text, "btn");
  e.type = "button";
  e.dataset.action = action;
  e.setAttribute("aria-label", label);
  return e;
}
