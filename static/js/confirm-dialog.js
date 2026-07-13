// confirm-dialog.js routes htmx's hx-confirm prompts through the app-styled
// #confirm-dialog instead of the OS-native window.confirm. htmx fires
// htmx:confirm for every request and exposes the hx-confirm text as
// detail.question (null when the element has none); we take over only when there
// is a question, show the dialog, and proceed via detail.issueRequest(true) on
// accept. When the triggering element also carries data-confirm-name, the dialog
// requires typing that name before Confirm enables (typed-name gate for the most
// destructive actions). Wired once; resolves elements by id each time so it
// survives hx-boost body swaps.
(function () {
  function open(question, requireName, onAccept) {
    var dlg = document.getElementById("confirm-dialog");
    var msg = document.getElementById("confirm-dialog-message");
    var accept = document.getElementById("confirm-dialog-accept");
    var cancel = document.getElementById("confirm-dialog-cancel");
    var nameRow = document.getElementById("confirm-dialog-name-row");
    var nameEl = document.getElementById("confirm-dialog-name");
    var input = document.getElementById("confirm-dialog-input");
    if (!dlg || !msg || !accept || !cancel) {
      // Dialog missing for some reason — fall back to native confirm so a
      // destructive action is never silently fired without a prompt. The
      // typed-name gate degrades to a plain confirm here; better a prompt of
      // the wrong shape than none.
      if (window.confirm(question)) onAccept();
      return;
    }
    // Already prompting (e.g. a double-click fired htmx:confirm twice): drop the
    // duplicate rather than stacking a second accept listener and calling
    // showModal() on an open dialog (which throws), which would make one Accept
    // fire both requests.
    if (dlg.open) return;
    msg.textContent = question;

    var gated = !!(requireName && nameRow && nameEl && input);
    if (nameRow) nameRow.classList.toggle("hidden", !gated);
    if (gated) {
      nameEl.textContent = requireName;
      input.value = "";
    }
    // The dimmed/unclickable styling rides on the button's disabled: classes
    // (confirm_dialog.templ), so only the property needs toggling here.
    accept.disabled = gated;

    function syncGate() {
      accept.disabled = input.value !== requireName;
    }
    function onEnter(e) {
      if (e.key === "Enter") {
        e.preventDefault();
        if (!accept.disabled) yes();
      }
    }

    function cleanup() {
      accept.removeEventListener("click", yes);
      cancel.removeEventListener("click", no);
      dlg.removeEventListener("cancel", no);
      if (gated) {
        input.removeEventListener("input", syncGate);
        input.removeEventListener("keydown", onEnter);
      }
    }
    function yes() {
      cleanup();
      dlg.close();
      onAccept();
    }
    function no() {
      cleanup();
      if (dlg.open) dlg.close();
    }
    accept.addEventListener("click", yes);
    cancel.addEventListener("click", no);
    dlg.addEventListener("cancel", no); // Esc dismiss
    if (gated) {
      input.addEventListener("input", syncGate);
      input.addEventListener("keydown", onEnter);
    }
    dlg.showModal();
    if (gated) {
      input.focus();
    } else {
      accept.focus();
    }
  }

  document.addEventListener("htmx:confirm", function (e) {
    // No hx-confirm on the element → let htmx proceed untouched.
    if (!e.detail || !e.detail.question) return;
    e.preventDefault();
    var requireName = e.detail.elt ? e.detail.elt.getAttribute("data-confirm-name") : null;
    open(e.detail.question, requireName, function () {
      e.detail.issueRequest(true);
    });
  });
})();
