// Only SGR styling is interpreted. Terminal text is always inserted as text,
// never HTML; escape sequences cannot become markup, links or scripts.
export function ansiFragment(text, document) {
  const fragment = document.createDocumentFragment();
  const colors = ["#161a22", "#f7768e", "#4ec9a5", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#e6e6e6"];
  let style = {};
  for (const part of text.split(/(\x1b\[[\d;]*m)/g)) {
    if (part.startsWith("\x1b[")) {
      const codes = part.slice(2, -1).split(";").map(Number);
      for (let i = 0; i < codes.length; i++) {
        const c = codes[i];
        if (c === 0) style = {};
        if (c === 1) style.fontWeight = "700";
        if (c === 22) delete style.fontWeight;
        if (c === 3) style.fontStyle = "italic";
        if (c === 23) delete style.fontStyle;
        if (c === 4) style.textDecoration = "underline";
        if (c === 24) delete style.textDecoration;
        if (c === 39) delete style.color;
        if (c === 49) delete style.backgroundColor;
        if (c >= 30 && c <= 37) style.color = colors[c - 30];
        if (c >= 40 && c <= 47) style.backgroundColor = colors[c - 40];
        if ((c === 38 || c === 48) && codes[i + 1] === 2) {
          style[c === 38 ? "color" : "backgroundColor"] = `rgb(${codes.slice(i + 2, i + 5).join(",")})`;
          i += 4;
        }
      }
      continue;
    }
    if (!part) continue;
    const span = document.createElement("span");
    Object.assign(span.style, style);
    span.textContent = part.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "");
    fragment.append(span);
  }
  return fragment;
}

const root = typeof document === "undefined" ? null : document.querySelector("[data-browser-demo]");
if (root) {
  const terminal = root.querySelector(".demo-screen");
  const status = root.querySelector(".demo-runtime-status");
  const start = root.querySelector("[data-demo-start]");
  const select = root.querySelector("#demo-screen");
  const input = root.querySelector("#demo-text");
  let worker, ready = false, latest;
  const viewport = root.querySelector(".demo-viewport");
  const measure = document.createElement("canvas").getContext("2d");
  // One glyph is not enough to size a column: the rounding error it hides is
  // a fraction of a pixel per cell, which is a whole column across a hundred
  // and thirty of them — and a column too many is a header cut off at the
  // right edge. Measure a long run and divide.
  const metrics = () => {
    const style = getComputedStyle(terminal);
    measure.font = style.font;
    const sample = "M".repeat(50);
    return { cell: measure.measureText(sample).width / sample.length || 8.4, line: parseFloat(style.lineHeight) || 22 };
  };
  const inner = (side) => {
    const style = getComputedStyle(viewport);
    return side === "width"
      ? viewport.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
      : viewport.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom);
  };
  const clamp = (value, lo, hi) => Math.max(lo, Math.min(hi, value));
  const dimensions = () => {
    terminal.style.fontSize = "";
    let { cell, line } = metrics();
    const width = inner("width");
    // Correlux itself refuses to draw below sixty columns, so a phone cannot
    // be given fewer. It can be given smaller type: a frame that fits is
    // readable, and one that has to be dragged sideways is not.
    if (Math.floor(width / cell) < 60) {
      const size = (parseFloat(getComputedStyle(terminal).fontSize) * width) / (60 * cell);
      terminal.style.fontSize = `${Math.max(8, size).toFixed(2)}px`;
      ({ cell, line } = metrics());
    }
    // Expanded, the terminal owns the window and is sized by it; inline it
    // keeps a fixed, readable height so the page below does not move.
    const height = root.classList.contains("expanded") ? clamp(Math.floor(inner("height") / line), 20, 60) : 30;
    return { width: clamp(Math.floor(width / cell), 60, 180), height };
  };
  function send(data, focus = false) {
    if (!ready) return;
    worker.postMessage({ input: data });
    if (focus) terminal.focus({ preventScroll: true });
  }
  function fail(message) {
    ready = false;
    status.textContent = `${message} Try again or read the user guide.`;
    root.querySelector(".demo-start").hidden = false;
    start.disabled = false;
    start.textContent = "Retry terminal →";
    root.querySelectorAll("[data-demo-key], #demo-screen, #demo-text, .demo-input button, [data-demo-reset]").forEach(el => el.disabled = true);
    worker?.terminate();
  }
  start.addEventListener("click", () => {
    start.disabled = true;
    start.textContent = "Starting terminal…";
    status.textContent = "Loading the Go terminal runtime…";
    worker = new Worker(new URL("demo-worker.js", import.meta.url));
    worker.onerror = () => fail("The terminal runtime could not load.");
    worker.onmessage = ({ data }) => {
      if (data.error) return fail(data.error);
      const first = !ready;
      ready = true;
      latest = data.frame;
      terminal.replaceChildren(ansiFragment(latest.ansi, document));
      terminal.dataset.view = latest.view;
      terminal.dataset.overlay = latest.overlay;
      root.querySelector(".demo-start").hidden = true;
      root.querySelectorAll("[data-demo-key], #demo-screen, #demo-text, .demo-input button, [data-demo-reset]").forEach(el => el.disabled = false);
      status.textContent = `${latest.context} · ${latest.width} × ${latest.height} · demo data`;
      if (first) terminal.focus({ preventScroll: true });
    };
    worker.postMessage({ boot: true, input: dimensions() });
  });
  select.addEventListener("change", () => send({ screen: select.value }, true));
  root.querySelectorAll("[data-demo-key]").forEach(button => button.addEventListener("click", () => send({ key: button.dataset.demoKey }, true)));
  root.querySelector("[data-demo-reset]").addEventListener("click", () => {
    select.value = "apps"; input.value = ""; send({ screen: "reset", ...dimensions() }, true);
  });
  root.querySelector("[data-demo-expand]").addEventListener("click", (event) => {
    const expanded = root.classList.toggle("expanded");
    event.currentTarget.setAttribute("aria-pressed", expanded);
    event.currentTarget.textContent = expanded ? "Collapse ↙" : "Expand ↗";
    send(dimensions());
  });
  root.querySelector(".demo-input").addEventListener("submit", (event) => {
    event.preventDefault();
    send({ text: input.value, replace: true, key: "enter" });
    input.value = "";
  });
  terminal.addEventListener("keydown", (event) => {
    if (!ready || event.key === "Tab" || event.isComposing) return;
    // Preserve normal browser copy and zoom. Browser-reserved shortcuts also
    // have explicit controls so they remain available on mobile and Safari.
    if ((event.metaKey || event.ctrlKey) && ["c", "+", "-", "0", "="].includes(event.key.toLowerCase())) return;
    const names = { ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right", Enter: "enter", Escape: "esc", Backspace: "backspace", Delete: "delete", Home: "home", End: "end", PageUp: "pgup", PageDown: "pgdown" };
    let key = names[event.key] || (event.key.length === 1 ? event.key : "");
    if (!key) return;
    if (event.ctrlKey || event.metaKey) key = "ctrl+" + key.toLowerCase();
    else if (event.altKey) key = "alt+" + key;
    event.preventDefault();
    send({ key });
  });
  terminal.addEventListener("click", event => {
    if (!ready || window.getSelection()?.toString()) return;
    const rect = terminal.getBoundingClientRect();
    const { cell, line } = metrics();
    send({ click: [Math.floor((event.clientX - rect.left) / cell), Math.floor((event.clientY - rect.top) / line)] });
  });
  let resizeTimer;
  new ResizeObserver(() => { clearTimeout(resizeTimer); resizeTimer = setTimeout(() => send(dimensions()), 120); }).observe(root);
}
