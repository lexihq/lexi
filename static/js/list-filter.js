// list-filter.js wires the instances toolbar: a client-side text filter, a
// status filter, and click-to-sort on headers marked data-sort. The list is a
// single page, so filtering and sorting are pure DOM operations; state lives
// here so it survives the idle refresh swapping #instances-table under the
// toolbar (htmx:afterSwap re-applies it to the fresh rows). Delegated +
// idempotent, same as bulk-actions.js.
(function () {
  var sortState = { index: -1, dir: 1, type: "text" };

  function table() {
    return document.getElementById("instances-table");
  }
  function dataRows(t) {
    // Rows with a bulk checkbox are instance rows; the empty-state row is not.
    return Array.prototype.slice.call(t.querySelectorAll("tbody tr")).filter(function (tr) {
      return tr.querySelector("[data-bulk-cb]");
    });
  }

  function apply() {
    var t = table();
    if (!t) return;
    var input = document.querySelector("[data-list-filter]");
    var status = document.querySelector("[data-status-filter]");
    var q = input ? input.value.trim().toLowerCase() : "";
    var st = status ? status.value : "";
    dataRows(t).forEach(function (tr) {
      // Status matches its own column (cell 2), not the whole row: "Running"
      // as free text could sit in an image alias or tag.
      var hit = (!q || tr.textContent.toLowerCase().indexOf(q) !== -1) &&
        (!st || cellText(tr, 2).indexOf(st) !== -1);
      tr.style.display = hit ? "" : "none";
      if (!hit) {
        // A hidden row must not stay silently selected: bulk actions post every
        // checked box, visible or not, and acting on rows the user can't see is
        // exactly the trap the filter would otherwise set.
        var cb = tr.querySelector("[data-bulk-cb]");
        if (cb && cb.checked) {
          cb.checked = false;
          cb.dispatchEvent(new Event("change", { bubbles: true }));
        }
      }
    });
    applySort(t);
  }

  function applySort(t) {
    if (sortState.index < 0) return;
    var tbody = t.querySelector("tbody");
    if (!tbody) return;
    var rows = dataRows(t);
    rows.sort(function (a, b) {
      var av = cellText(a, sortState.index);
      var bv = cellText(b, sortState.index);
      if (sortState.type === "num") {
        return (parseFloat(av) || 0) - (parseFloat(bv) || 0) || 0;
      }
      return av.localeCompare(bv, undefined, { numeric: true, sensitivity: "base" });
    });
    if (sortState.dir < 0) rows.reverse();
    rows.forEach(function (tr) {
      tbody.appendChild(tr);
    });
    // Reflect the active sort on the fresh headers (the table body is swapped
    // whole every 15s, wiping any previous aria-sort).
    var heads = t.querySelectorAll("thead th");
    heads.forEach(function (th, i) {
      if (!th.hasAttribute("data-sort")) return;
      if (i === sortState.index) {
        th.setAttribute("aria-sort", sortState.dir > 0 ? "ascending" : "descending");
      } else {
        th.removeAttribute("aria-sort");
      }
    });
  }

  function cellText(tr, i) {
    var cell = tr.cells[i];
    return cell ? cell.textContent.trim() : "";
  }

  document.addEventListener("input", function (e) {
    if (e.target.matches("[data-list-filter]")) apply();
  });
  document.addEventListener("change", function (e) {
    if (e.target.matches("[data-status-filter]")) apply();
  });
  function onSortActivate(th) {
    var index = th.cellIndex;
    if (sortState.index === index) {
      sortState.dir = -sortState.dir;
    } else {
      sortState = { index: index, dir: 1, type: th.getAttribute("data-sort") };
    }
    var t = table();
    if (t) applySort(t);
  }
  document.addEventListener("click", function (e) {
    var th = e.target.closest ? e.target.closest("th[data-sort]") : null;
    if (th && table() && table().contains(th)) onSortActivate(th);
  });
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    var th = e.target.closest ? e.target.closest("th[data-sort]") : null;
    if (th && table() && table().contains(th)) {
      e.preventDefault();
      onSortActivate(th);
    }
  });

  // The idle refresh (and bulk actions) replace #instances-table wholesale;
  // re-apply the active filter and sort to the fresh rows.
  document.body.addEventListener("htmx:afterSwap", function (e) {
    if (e.target && (e.target.id === "instances-table" || e.target.querySelector("#instances-table"))) apply();
  });
})();
