// Theme handling. Loaded synchronously in <head> so the stored preferences are
// applied to <html> before first paint (no flash). Two orthogonal axes both
// live on <html> (which hx-boost never swaps, so they survive boosted nav):
//   - mode:  the .dark class (light/dark), toggled by toggleTheme()
//   - color: the data-theme attribute (palette), set by setColorTheme()
(function () {
  // Palettes selectable in the header picker. "" is the built-in default (no
  // data-theme attribute); the rest map to [data-theme="..."] blocks in the CSS.
  var COLOR_THEMES = ["", "ocean"];

  function apply(dark) {
    document.documentElement.classList.toggle("dark", dark);
  }
  function preferred() {
    try {
      var stored = localStorage.getItem("theme");
      if (stored === "dark") return true;
      if (stored === "light") return false;
    } catch (e) {}
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }

  function preferredColor() {
    try {
      var stored = localStorage.getItem("color-theme");
      if (COLOR_THEMES.indexOf(stored) !== -1) return stored;
    } catch (e) {}
    return "";
  }
  function applyColor(name) {
    if (name) {
      document.documentElement.setAttribute("data-theme", name);
    } else {
      document.documentElement.removeAttribute("data-theme");
    }
    // Mark the active option in any open picker (server render can't know the
    // client's stored choice, so the check rides on this instead).
    var opts = document.querySelectorAll("[data-theme-option]");
    for (var i = 0; i < opts.length; i++) {
      var active = opts[i].getAttribute("data-theme-option") === name;
      opts[i].setAttribute("aria-checked", active ? "true" : "false");
    }
  }

  // Canvas-drawn UI (uPlot charts, xterm) can't use CSS tokens, so notify it to
  // re-read theme colors and redraw. DOM/Tailwind elements re-theme via the
  // class/attribute alone and ignore this.
  function notifyChange() {
    var dark = document.documentElement.classList.contains("dark");
    window.dispatchEvent(new CustomEvent("lexi:themechange", { detail: { dark: dark } }));
  }

  apply(preferred());
  applyColor(preferredColor());
  // Re-sync active markers after boosted navigations re-render the header.
  document.addEventListener("htmx:afterSettle", function () {
    applyColor(preferredColor());
  });

  window.toggleTheme = function () {
    var dark = !document.documentElement.classList.contains("dark");
    apply(dark);
    try {
      localStorage.setItem("theme", dark ? "dark" : "light");
    } catch (e) {}
    notifyChange();
  };

  window.setColorTheme = function (name) {
    if (COLOR_THEMES.indexOf(name) === -1) name = "";
    applyColor(name);
    try {
      localStorage.setItem("color-theme", name);
    } catch (e) {}
    notifyChange();
  };
})();
