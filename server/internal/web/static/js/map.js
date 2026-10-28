// The placement map (docs/spec/admin-ui.md 5.6): the installation seen from
// above, players on the outer ring and Bots inside, joined by the 24 routes.
// It shows state only and has no controls. The skeleton is made once; the
// polls change only classes and the texts.

import { svgElement } from "./dom.js";
import { BOT_FIFO_MAX, BOT_IDS, PLAYER_IDS, TARGETS, displayName } from "./labels.js";

const svg = document.getElementById("map");

// The orientation of demo/network_game_sim.html: P1 at the top, a little to
// the left, then P2-P8 clockwise; B1 at the top, B2 lower right, B3 lower
// left. How it faces the real installation is still open (admin-ui.md 14).
const CX = 180, CY = 103, R = 81, RB = 35;
const positions = {};
PLAYER_IDS.forEach((id, i) => {
  const a = -Math.PI / 2 - Math.PI / 8 + (i * Math.PI) / 4;
  positions[id] = { x: CX + R * Math.cos(a), y: CY + R * Math.sin(a), a };
});
BOT_IDS.forEach((id, i) => {
  const a = -Math.PI / 2 + (i * 2 * Math.PI) / 3;
  positions[id] = { x: CX + RB * Math.cos(a), y: CY + RB * Math.sin(a), a };
});

// The Bots each player links to, besides its two neighbours, as in
// demo/network_game_sim.html.
const botLinks = {
  P1: ["B1", "B3"], P2: ["B1", "B2"], P3: ["B2", "B3"], P4: ["B1", "B2"],
  P5: ["B1", "B3"], P6: ["B1", "B2"], P7: ["B1", "B3"], P8: ["B2", "B3"],
};

// spots maps "kind:nodeId" of each target to its shapes.
const spots = new Map();
const fifoSlots = new Map();

function build() {
  const p = (n) => n.toFixed(2);
  svg.append(svgElement("circle", { class: "ring", cx: CX, cy: CY, r: R }));
  PLAYER_IDS.forEach((id, i) => {
    const a = positions[id];
    for (const other of [PLAYER_IDS[(i + 1) % PLAYER_IDS.length], ...botLinks[id]]) {
      const b = positions[other];
      svg.append(svgElement("line", { class: "link", x1: p(a.x), y1: p(a.y), x2: p(b.x), y2: p(b.y) }));
    }
  });
  for (const id of PLAYER_IDS) {
    const { x, y, a } = positions[id];
    const disc = svgElement("circle", { class: "disc", cx: p(x), cy: p(y), r: 15 });
    addSpot("player", id, x, y, disc, letter(id, x, y), 11, 21, 19, "circle");
    if (TARGETS.led.includes(id)) {
      addBadge(id, x + 27 * Math.cos(a), y + 27 * Math.sin(a));
    }
  }
  // A Bot has no board of its own: its square stands for the LED board of
  // the same ID, which drives its links and its signal tower.
  for (const id of BOT_IDS) {
    const { x, y } = positions[id];
    const slots = [];
    for (let i = 0; i < BOT_FIFO_MAX; i++) {
      const a0 = -Math.PI / 2 + (i * 2 * Math.PI) / BOT_FIFO_MAX + 0.18;
      const a1 = a0 + (2 * Math.PI) / BOT_FIFO_MAX - 0.36;
      const at = (angle) => `${p(x + 19 * Math.cos(angle))} ${p(y + 19 * Math.sin(angle))}`;
      const slot = svgElement("path", { class: "slot", d: `M ${at(a0)} A 19 19 0 0 1 ${at(a1)}` });
      svg.append(slot);
      slots.push(slot);
    }
    fifoSlots.set(id, slots);
    const disc = svgElement("rect", { class: "disc", x: p(x - 12), y: p(y - 12), width: 24, height: 24, rx: 4 });
    addSpot("led", id, x, y, disc, letter(id, x, y), 10, 24, 16, "rect");
  }
}

function letter(id, x, y) {
  const text = svgElement("text", { class: "letter", x: x.toFixed(2), y: y.toFixed(2) });
  text.textContent = displayName(id);
  return text;
}

// addSpot adds a target drawn as a disc or a square, with its cross for a
// lost board and its rings for the selected and the previewed device.
function addSpot(kind, id, x, y, disc, label, crossAt, previewSize, selectSize, shape) {
  const group = svgElement("g", { "data-node": id });
  const cross = svgElement("g", { class: "x" });
  cross.append(svgElement("circle", { cx: (x + crossAt).toFixed(2), cy: (y + crossAt).toFixed(2), r: 6.5 }));
  const mark = svgElement("text", { x: (x + crossAt).toFixed(2), y: (y + crossAt + 0.5).toFixed(2) });
  mark.textContent = "×";
  cross.append(mark);
  const previewRing = ring(shape, "pv-ring", x, y, previewSize);
  const selectRing = ring(shape, "sel", x, y, selectSize);
  group.append(disc, label, cross, previewRing, selectRing);
  svg.append(group);
  spots.set(`${kind}:${id}`, { shapes: [disc, label], cross, previewRing, selectRing });
}

function ring(shape, className, x, y, size) {
  if (shape === "circle") {
    return svgElement("circle", { class: className, cx: x.toFixed(2), cy: y.toFixed(2), r: size });
  }
  return svgElement("rect", {
    class: className, x: (x - size).toFixed(2), y: (y - size).toFixed(2), width: size * 2, height: size * 2, rx: 6,
  });
}

// The small "L" outside a player disc is the LED board of the same ID.
function addBadge(id, x, y) {
  const group = svgElement("g", { class: "badge" });
  const box = svgElement("rect", { x: (x - 7).toFixed(2), y: (y - 6).toFixed(2), width: 14, height: 12, rx: 2 });
  const label = svgElement("text", { x: x.toFixed(2), y: (y + 0.5).toFixed(2) });
  label.textContent = "L";
  const previewRing = svgElement("rect", {
    class: "pv-ring", x: (x - 11).toFixed(2), y: (y - 10).toFixed(2), width: 22, height: 20, rx: 4,
  });
  const selectRing = svgElement("rect", {
    class: "sel", x: (x - 10).toFixed(2), y: (y - 9).toFixed(2), width: 20, height: 18, rx: 3,
  });
  group.append(box, label, previewRing, selectRing);
  svg.append(group);
  spots.set(`led:${id}`, { badge: group, label, previewRing, selectRing });
}

build();

// renderMap shows the boards of the targets, the Bot FIFOs, the selected
// device and the previewed one. A temporary preview of an unassigned board
// is not drawn, since the board is not at that place.
export function renderMap(snapshot, selectedId) {
  const { devices, bots, preview } = snapshot;
  const at = (device) => device?.assignment ? `${device.assignment.kind}:${device.assignment.nodeId}` : null;
  const selectedAt = at(devices.find((d) => d.deviceId === selectedId));
  const previewAt = preview ? at(devices.find((d) => d.deviceId === preview.deviceId)) : null;
  const boards = new Map(devices.filter((d) => d.assignment).map((d) => [at(d), d]));

  for (const [key, spot] of spots) {
    const board = boards.get(key);
    const state = !board ? "empty" : board.connected ? "" : "gone";
    if (spot.badge) {
      setClass(spot.badge, `badge ${state}`.trim());
      spot.label.textContent = state === "gone" ? "×" : "L";
    } else {
      for (const shape of spot.shapes) {
        setClass(shape, `${shape.tagName === "text" ? "letter" : "disc"} ${state}`.trim());
      }
      spot.cross.classList.toggle("on", state === "gone");
    }
    spot.selectRing.classList.toggle("on", key === selectedAt);
    // The blink class changes only with the target, which keeps its cycle.
    spot.previewRing.classList.toggle("on", key === previewAt);
  }
  for (const bot of bots) {
    for (const [i, slot] of (fifoSlots.get(bot.nodeId) ?? []).entries()) {
      slot.classList.toggle("on", i < bot.fifoLength);
    }
  }
}

function setClass(element, value) {
  if (element.getAttribute("class") !== value) {
    element.setAttribute("class", value);
  }
}
