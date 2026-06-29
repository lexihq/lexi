// bulk-actions.js wires the instances table's multi-select: a select-all
// header box, per-row boxes, and a bulk-action bar that appears only while at
// least one row is selected. It owns no submission logic — the bar's buttons
// post via htmx (hx-include="[data-bulk-cb]"); this just manages selection UI.
// Delegated + idempotent so it survives the table fragment being swapped by a
// bulk action or a single-row update.
(function () {
  function sync() {
    const boxes = Array.prototype.slice.call(document.querySelectorAll("[data-bulk-cb]"));
    const checked = boxes.filter((b) => b.checked);
    const bar = document.querySelector("[data-bulk-bar]");
    const all = document.querySelector("[data-bulk-all]");
    const count = document.querySelector("[data-bulk-count]");
    if (bar) {
      // Toggle both so the shown state is display:flex (gap/alignment apply),
      // not the default block left behind by only removing `hidden`.
      bar.classList.toggle("hidden", checked.length === 0);
      bar.classList.toggle("flex", checked.length > 0);
    }
    if (count) count.textContent = checked.length + " selected";
    if (all) {
      all.checked = boxes.length > 0 && checked.length === boxes.length;
      all.indeterminate = checked.length > 0 && checked.length < boxes.length;
    }
  }

  document.addEventListener("change", function (e) {
    const t = e.target;
    if (t.matches("[data-bulk-all]")) {
      document.querySelectorAll("[data-bulk-cb]").forEach((b) => {
        b.checked = t.checked;
      });
    }
    if (t.matches("[data-bulk-cb], [data-bulk-all]")) sync();
  });

  // After a bulk action (or any swap) replaces the table, the new boxes are
  // unchecked — re-sync so the bar hides and the count resets.
  document.body.addEventListener("htmx:afterSwap", sync);
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", sync);
  } else {
    sync();
  }

  // Idle live-refresh: while the instances list is shown and nothing is
  // selected, refresh the table so status and CPU sparklines stay current
  // without a manual reload. Paused during selection so it never clobbers an
  // in-progress bulk choice; a no-op off the list page (no #instances-table).
  setInterval(function () {
    const table = document.getElementById("instances-table");
    if (!table || !window.htmx) return;
    if (document.querySelector("[data-bulk-cb]:checked")) return;
    // Don't swap the table out from under an open row dialog or kebab menu: the
    // rename/move/clone/migrate dialogs live inside the table, so a refresh would
    // close the overlay and discard any half-typed input mid-edit.
    if (table.querySelector("dialog[open], :popover-open")) return;
    window.htmx.ajax("GET", "/partials/instances", { target: "#instances-table", swap: "outerHTML" });
  }, 15000);
})();
