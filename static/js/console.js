// console.js wires the vendored xterm.js terminal to the lxcon exec WebSocket.
// Wire protocol (matches internal/server consoleWS):
//   client → server  binary frame  = stdin bytes
//   client → server  text frame    = {"cols":N,"rows":M} resize
//   server → client  binary frame  = stdout bytes
(function () {
  const el = document.getElementById("terminal");
  if (!el || typeof Terminal === "undefined") {
    return;
  }

  const term = new Terminal({ cursorBlink: true, fontFamily: "monospace", fontSize: 13 });
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

  ws.onclose = function () {
    term.write("\r\n\x1b[31m[connection closed]\x1b[0m\r\n");
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
})();
