// Theme (light/dark) handling. Loaded synchronously in <head> so the stored
// preference is applied to <html> before first paint (no flash). The .dark
// class and this global function live on <html>/window, which hx-boost never
// swaps, so the toggle keeps working across boosted navigations.
(function () {
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
  apply(preferred());
  window.toggleTheme = function () {
    var dark = !document.documentElement.classList.contains("dark");
    apply(dark);
    try {
      localStorage.setItem("theme", dark ? "dark" : "light");
    } catch (e) {}
    // Canvas-drawn UI (uPlot charts, xterm) can't use CSS tokens, so notify it to
    // re-read theme colors and redraw. DOM/Tailwind elements re-theme via the
    // .dark class alone and ignore this.
    window.dispatchEvent(new CustomEvent("lexi:themechange", { detail: { dark: dark } }));
  };
})();
