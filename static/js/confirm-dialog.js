// confirm-dialog.js routes htmx's hx-confirm prompts through the app-styled
// #confirm-dialog instead of the OS-native window.confirm. htmx fires
// htmx:confirm for every request and exposes the hx-confirm text as
// detail.question (null when the element has none); we take over only when there
// is a question, show the dialog, and proceed via detail.issueRequest(true) on
// accept. Wired once; resolves elements by id each time so it survives hx-boost
// body swaps.
(function () {
  function open(question, onAccept) {
    var dlg = document.getElementById("confirm-dialog");
    var msg = document.getElementById("confirm-dialog-message");
    var accept = document.getElementById("confirm-dialog-accept");
    var cancel = document.getElementById("confirm-dialog-cancel");
    if (!dlg || !accept || !cancel) {
      // Dialog missing for some reason — fall back to native confirm so a
      // destructive action is never silently fired without a prompt.
      if (window.confirm(question)) onAccept();
      return;
    }
    // Already prompting (e.g. a double-click fired htmx:confirm twice): drop the
    // duplicate rather than stacking a second accept listener and calling
    // showModal() on an open dialog (which throws), which would make one Accept
    // fire both requests.
    if (dlg.open) return;
    msg.textContent = question;

    function cleanup() {
      accept.removeEventListener("click", yes);
      cancel.removeEventListener("click", no);
      dlg.removeEventListener("cancel", no);
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
    dlg.addEventListener("cancel", no); // Esc / backdrop dismiss
    dlg.showModal();
    accept.focus();
  }

  document.addEventListener("htmx:confirm", function (e) {
    // No hx-confirm on the element → let htmx proceed untouched.
    if (!e.detail || !e.detail.question) return;
    e.preventDefault();
    open(e.detail.question, function () {
      e.detail.issueRequest(true);
    });
  });
})();
