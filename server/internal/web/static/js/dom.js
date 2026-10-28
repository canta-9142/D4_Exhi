// DOM helpers shared by the admin page modules. Every value from the server
// goes into the page through textContent or attributes, never through HTML
// (docs/spec/admin-ui.md 12). The helpers change the page only when a value
// differs, so that the 500ms polls keep open selects, typed values, focus and
// running animations.

// setText leaves equal text alone, so that a live region announces only
// changes and a selection in unchanged text survives the next poll.
export function setText(element, text) {
  if (element.textContent !== text) {
    element.textContent = text;
  }
}

// setAttr sets an attribute only when it changes. null removes it.
export function setAttr(element, name, value) {
  if (value === null) {
    element.removeAttribute(name);
  } else if (element.getAttribute(name) !== value) {
    element.setAttribute(name, value);
  }
}

export function element(tag, text = "", className = "") {
  const e = document.createElement(tag);
  e.textContent = text;
  if (className) {
    e.className = className;
  }
  return e;
}

const SVG_NS = "http://www.w3.org/2000/svg";

export function svgElement(tag, attrs = {}) {
  const e = document.createElementNS(SVG_NS, tag);
  for (const [name, value] of Object.entries(attrs)) {
    e.setAttribute(name, String(value));
  }
  return e;
}

// tile is the colour tile of a node (admin-ui.md 7.1). Its name is also
// written next to it, so the tile itself is hidden from screen readers.
// variant is "" for a filled tile, "led" for an outlined one, "empty" for no
// node and "temp" for a temporary preview.
export function tile(nodeId, name, variant = "", small = false) {
  const e = element("span", name, ["tile", variant, small ? "sm" : ""].filter(Boolean).join(" "));
  if (nodeId) {
    e.dataset.node = nodeId;
  }
  e.setAttribute("aria-hidden", "true");
  return e;
}

// chip is a tile followed by its label.
export function chip(nodeId, name, label, variant = "", labelClass = "") {
  const e = element("span", "", "node");
  e.append(tile(nodeId, name, variant, true), element("span", label, labelClass));
  return e;
}

// fieldError is where an operation that sends nothing says why, next to its
// input or select.
export function fieldError() {
  const e = element("span", "", "field-error");
  e.setAttribute("aria-live", "polite");
  return e;
}

// setContent replaces the children of container only when key changes. It
// is for small parts, such as a chip, whose content is rebuilt as a whole;
// rebuilding them only on a change keeps their animations in phase.
const contentKeys = new WeakMap();

export function setContent(container, key, build) {
  if (contentKeys.get(container) !== key) {
    contentKeys.set(container, key);
    container.replaceChildren(...build());
  }
}

// placeChildren makes children the children of parent in this order. Nodes
// already in place are not moved, so that a focused control inside them
// keeps the focus.
export function placeChildren(parent, children) {
  children.forEach((child, i) => {
    if (parent.children[i] !== child) {
      parent.insertBefore(child, parent.children[i] ?? null);
    }
  });
  while (parent.children.length > children.length) {
    parent.lastElementChild.remove();
  }
}

// setOptions sets the options of a select from [{value, text}]. The options
// are rebuilt only when the list changes, and never while the select has the
// focus, since that would close an open list; the latest list is applied on
// blur. The chosen value is kept while it is still an option.
const optionKeys = new WeakMap();
const pendingOptions = new WeakMap();

export function setOptions(select, options) {
  const key = JSON.stringify(options);
  if (optionKeys.get(select) === key) {
    pendingOptions.delete(select);
    return;
  }
  if (document.activeElement === select) {
    if (!pendingOptions.has(select)) {
      select.addEventListener("blur", () => {
        const latest = pendingOptions.get(select);
        pendingOptions.delete(select);
        if (latest) {
          setOptions(select, latest);
        }
      }, { once: true });
    }
    pendingOptions.set(select, options);
    return;
  }
  const value = select.value;
  select.replaceChildren(...options.map((option) => {
    const e = element("option", option.text);
    e.value = option.value;
    return e;
  }));
  optionKeys.set(select, key);
  select.value = options.some((option) => option.value === value) ? value : "";
}
