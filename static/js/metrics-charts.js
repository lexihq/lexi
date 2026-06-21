// metrics-charts.js renders the instance metrics history as uPlot line charts.
// It is self-wiring: it scans for a #metrics-charts container on initial load
// and after every HTMX swap, so it works whether the metrics tab arrives as a
// full page or an htmx-swapped fragment. On its next 3s tick after the container
// leaves the DOM (tab change or navigation) the poll loop clears its interval,
// destroys the charts, and removes its resize listener, so nothing leaks.
(function () {
  const POLL_MS = 3000;

  function fmtBytes(n) {
    if (n == null) return "";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return (i === 0 ? n : n.toFixed(1)) + " " + units[i];
  }

  // token reads a themed CSS custom property off <html>. The whole UI is authored
  // in oklch tokens, so the browser already parses oklch for canvas fillStyle —
  // we can hand uPlot the token string directly. Read live (not cached) so a
  // theme toggle + redraw picks up the new value. uPlot accepts a function for
  // axis/grid/tick stroke and calls it on every draw, so these re-theme for free.
  function token(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }
  const axisColor = () => token("--muted-foreground"); // tick labels + axis line
  const gridColor = () => token("--border"); //            grid lines + tick marks

  // ySize reserves just enough left gutter for the widest formatted y label.
  // The old fixed 56px clipped byte labels ("400.0 MiB" ≈ 58px) to garbage like
  // "3.7 MiB"; measuring the actual labels fits any unit (%, MiB, GiB, TiB).
  function ySize(u, values) {
    if (!values || !values.length) return 50;
    const pr = u.pxRatio || window.devicePixelRatio || 1;
    const ctx = u.ctx;
    const prev = ctx.font;
    ctx.font = Math.round(12 * pr) + "px system-ui, -apple-system, sans-serif";
    let max = 0;
    for (const v of values) {
      const w = ctx.measureText(v).width;
      if (w > max) max = w;
    }
    ctx.font = prev;
    return Math.ceil(max / pr) + 18; // + tick + gap
  }

  // makeChart builds a uPlot bound to a container's width, with a formatter for
  // the y-axis/value labels. series is the array of uPlot series specs after x.
  function makeChart(el, series, fmtY) {
    const themedAxis = (extra) =>
      Object.assign(
        { stroke: axisColor, grid: { stroke: gridColor }, ticks: { stroke: gridColor } },
        extra,
      );
    const opts = {
      width: el.clientWidth || 320,
      height: 160,
      legend: { show: true },
      scales: { x: { time: true } },
      axes: [
        themedAxis({}),
        themedAxis({ size: ySize, values: (u, vals) => vals.map(fmtY) }),
      ],
      series: [{}].concat(series),
    };
    return new uPlot(opts, [[]], el);
  }

  function initContainer(root) {
    if (!root || root.dataset.inited) return;
    if (typeof uPlot === "undefined") {
      // Asset failed to load (404, CSP). Leave inited unset so a later scan can
      // retry, but say so rather than rendering silent empty boxes.
      console.error("metrics charts: uPlot not loaded; charts disabled");
      return;
    }
    root.dataset.inited = "1";

    const stroke = {
      blue: "#3b82f6",
      slate: "#94a3b8",
      green: "#22c55e",
      orange: "#f97316",
    };
    const pct = (v) => (v == null ? "" : v.toFixed(0) + "%");

    const charts = [
      {
        el: root.querySelector("#mc-cpu"),
        chart: null,
        series: [{ label: "CPU", stroke: stroke.blue }],
        fmtY: pct,
        data: (d) => [d.t, d.cpu],
      },
      {
        el: root.querySelector("#mc-mem"),
        chart: null,
        series: [
          { label: "Used", stroke: stroke.blue },
          { label: "Total", stroke: stroke.slate },
        ],
        fmtY: fmtBytes,
        data: (d) => [d.t, d.memUsed, d.memTotal],
      },
      {
        el: root.querySelector("#mc-net"),
        chart: null,
        series: [
          { label: "RX", stroke: stroke.green },
          { label: "TX", stroke: stroke.orange },
        ],
        fmtY: fmtBytes,
        data: (d) => [d.t, d.rx, d.tx],
      },
    ];

    charts.forEach((c) => {
      if (c.el) c.chart = makeChart(c.el, c.series, c.fmtY);
    });

    function resize() {
      charts.forEach((c) => {
        if (c.chart && c.el) c.chart.setSize({ width: c.el.clientWidth, height: 160 });
      });
    }
    window.addEventListener("resize", resize);

    // The axis/grid stroke are functions reading live CSS tokens, so a theme
    // toggle just needs a redraw to recolor the canvas (the .dark class is
    // already on <html> by the time this fires).
    function retheme() {
      charts.forEach((c) => c.chart && c.chart.redraw());
    }
    window.addEventListener("lexi:themechange", retheme);

    function destroy() {
      window.removeEventListener("resize", resize);
      window.removeEventListener("lexi:themechange", retheme);
      charts.forEach((c) => c.chart && c.chart.destroy());
    }

    function tick() {
      if (!document.body.contains(root)) {
        clearInterval(timer);
        destroy();
        return;
      }
      fetch(root.dataset.seriesUrl, { headers: { "Hx-Request": "true" } })
        .then((r) => (r.ok ? r.json() : Promise.reject(new Error("HTTP " + r.status))))
        .then((d) => {
          charts.forEach((c) => c.chart && c.chart.setData(c.data(d)));
        })
        // Keep polling on failure (the next tick may recover), but surface it:
        // a frozen chart is otherwise indistinguishable from an idle instance,
        // and this also exposes bugs in the data mapping above.
        .catch((err) => console.warn("metrics charts: poll failed", err));
    }

    const timer = setInterval(tick, POLL_MS);
    tick();
  }

  function scan() {
    initContainer(document.getElementById("metrics-charts"));
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
  document.body.addEventListener("htmx:afterSettle", scan);
})();
