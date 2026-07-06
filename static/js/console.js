// console.js wires the vendored xterm.js terminal to the lexi exec WebSocket.
// Wire protocol (matches internal/server consoleWS):
//   client → server  binary frame  = stdin bytes
//   client → server  text frame    = {"cols":N,"rows":M} resize
//   server → client  binary frame  = stdout bytes
(function () {
  const el = document.getElementById("terminal");
  if (!el || typeof Terminal === "undefined") {
    return;
  }

  // xterm renders to a canvas and can't use CSS tokens, so theme.js's
  // lexi:themechange event is the signal to recolor it (see theme.js). Track
  // light/dark with a small explicit palette and refresh on toggle.
  function termTheme() {
    const dark = document.documentElement.classList.contains("dark");
    return dark
      ? { background: "#0a0a0a", foreground: "#e5e5e5", cursor: "#e5e5e5", selectionBackground: "#3f3f46" }
      : { background: "#ffffff", foreground: "#0a0a0a", cursor: "#0a0a0a", selectionBackground: "#d4d4d8" };
  }

  const term = new Terminal({ cursorBlink: true, fontFamily: "monospace", fontSize: 13, theme: termTheme() });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(el);
  fit.fit();

  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(scheme + "//" + location.host + el.dataset.ws);
  ws.binaryType = "arraybuffer";

  const encoder = new TextEncoder();

  function sendResize() {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ cols: term.cols, rows: term.rows }));
    }
  }

  ws.onopen = function () {
    sendResize();
    term.focus();
  };

  ws.onmessage = function (ev) {
    if (typeof ev.data === "string") {
      term.write(ev.data);
    } else {
      term.write(new Uint8Array(ev.data));
    }
  };

  ws.onclose = function (ev) {
    // The server packs the exec failure reason ("instance is not running")
    // into the close frame; show it instead of a bare "connection closed".
    const reason = ev && ev.reason ? ": " + ev.reason : "";
    term.write("\r\n\x1b[31m[connection closed" + reason + "]\x1b[0m\r\n");
  };

  term.onData(function (data) {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(encoder.encode(data));
    }
  });

  term.onResize(sendResize);
  window.addEventListener("resize", function () {
    fit.fit();
  });
  window.addEventListener("lexi:themechange", function () {
    term.options.theme = termTheme();
  });
})();
