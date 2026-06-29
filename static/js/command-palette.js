// command-palette.js drives the ⌘K / Ctrl-K quick switcher (#command-palette).
// It owns no data: at open time it scrapes the server-rendered sidebar for nav
// and instance links, so the list always matches the current capabilities and
// live instances. Navigation is a plain location change (works regardless of
// hx-boost). Wired once on load; the global key handler and window.* opener
// resolve the dialog by id each time, so they survive hx-boost body swaps.
(function () {
  function els() {
    return {
      dialog: document.getElementById("command-palette"),
      input: document.getElementById("cmdk-input"),
      list: document.getElementById("cmdk-results"),
      empty: document.getElementById("cmdk-empty"),
    };
  }

  // buildItems reads the sidebar's nav links and instance links. Deduped by href
  // so the active page (rendered twice) doesn't double up.
  function buildItems() {
    const items = [];
    const seen = new Set();
    const add = (label, href, kind) => {
      if (!label || !href || seen.has(kind + href)) return;
      seen.add(kind + href);
      items.push({ label: label, href: href, kind: kind });
    };
    document.querySelectorAll("aside nav a[href]").forEach((a) => {
      add(a.textContent.trim(), a.getAttribute("href"), "Page");
    });
    document.querySelectorAll('aside a[href^="/instances/"]').forEach((a) => {
      const name = (a.querySelector(".truncate") || a).textContent.trim();
      add(name, a.getAttribute("href"), "Instance");
    });
    return items;
  }

  let all = [];
  let shown = [];
  let active = 0;

  function render(filter) {
    const { list, empty, input } = els();
    const q = filter.trim().toLowerCase();
    shown = q ? all.filter((it) => it.label.toLowerCase().includes(q)) : all;
    active = 0;
    list.innerHTML = "";
    shown.forEach((it, i) => {
      const li = document.createElement("li");
      li.id = "cmdk-opt-" + i;
      li.setAttribute("role", "option");
      li.setAttribute("aria-selected", i === active ? "true" : "false");
      li.className =
        "flex cursor-pointer items-center justify-between gap-2 rounded-md px-3 py-2 text-sm aria-selected:bg-accent aria-selected:text-accent-foreground";
      li.innerHTML =
        '<span class="truncate"></span><span class="shrink-0 rounded border px-1.5 py-0.5 text-xs text-muted-foreground"></span>';
      li.firstChild.textContent = it.label;
      li.lastChild.textContent = it.kind;
      li.addEventListener("mousemove", () => setActive(i));
      li.addEventListener("click", () => activate(i));
      list.appendChild(li);
    });
    empty.classList.toggle("hidden", shown.length > 0);
    // Keep the combobox's active-descendant pointing at the highlighted option
    // (or clear it when there are none) so screen readers don't track a removed id.
    if (shown.length) input.setAttribute("aria-activedescendant", "cmdk-opt-" + active);
    else input.removeAttribute("aria-activedescendant");
  }

  function setActive(i) {
    const { list } = els();
    const opts = list.children;
    if (!opts.length) return;
    active = (i + opts.length) % opts.length;
    for (let n = 0; n < opts.length; n++) {
      opts[n].setAttribute("aria-selected", n === active ? "true" : "false");
    }
    els().input.setAttribute("aria-activedescendant", "cmdk-opt-" + active);
    opts[active].scrollIntoView({ block: "nearest" });
  }

  function activate(i) {
    const it = shown[i];
    if (!it) return;
    close();
    window.location.assign(it.href);
  }

  function open() {
    const { dialog, input } = els();
    if (!dialog || dialog.open) return;
    all = buildItems();
    input.value = "";
    render("");
    dialog.showModal();
    input.focus();
  }

  function close() {
    const { dialog } = els();
    if (dialog && dialog.open) dialog.close();
  }

  window.lexiOpenCommandPalette = open;

  // Global shortcut: Cmd/Ctrl-K. Ignored while typing elsewhere only if it isn't
  // the combo (the combo always wins, like every editor's palette).
  document.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
      // Don't steal Ctrl-K from the console terminal — there it's readline's
      // kill-to-end-of-line. The header button still opens the palette there.
      if (e.target && e.target.closest && e.target.closest("#terminal")) return;
      e.preventDefault();
      const { dialog } = els();
      if (dialog && dialog.open) close();
      else open();
    }
  });

  // In-dialog keys: arrows move, Enter activates. Esc/backdrop are native.
  document.addEventListener("keydown", (e) => {
    const { dialog } = els();
    if (!dialog || !dialog.open) return;
    // Mid-IME-composition Enter/arrows belong to the input method, not the list.
    if (e.isComposing) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(active + 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(active - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      activate(active);
    }
  });

  // Filter on the input event (fires for typing, paste, and IME) rather than
  // keydown, which misses non-key value changes. Delegated so it survives the
  // dialog being re-rendered by an hx-boost body swap.
  document.addEventListener("input", (e) => {
    if (e.target && e.target.id === "cmdk-input") render(e.target.value);
  });

  // Clicking the backdrop (outside the inner content) closes — native <dialog>
  // click lands on the dialog element itself for backdrop hits.
  document.addEventListener("click", (e) => {
    const { dialog } = els();
    if (dialog && dialog.open && e.target === dialog) close();
  });
})();
