// Surface backend errors (4xx/5xx) from htmx requests as a toast. HTMX 2 skips
// swapping non-2xx responses by default, so without this every mutating form
// silently swallows backend errors.
//
// We deliberately leave evt.detail.shouldSwap at its default (false): forcing the
// swap would (a) clobber the targeted table/form and (b) for a boosted request
// run its normal history update, corrupting the URL (e.g. a failed create on
// /networks/new would push /networks while the form stays). Instead we render the
// toast ourselves by appending it to <body>, where toast.js's MutationObserver
// initializes it. Only responses that are actually toast markup are inserted, so
// plain-text http.Error responses aren't appended as a stray text node.
document.body.addEventListener("htmx:beforeSwap", function (evt) {
  const xhr = evt.detail.xhr;
  if (!xhr || xhr.status < 400) return;
  const body = xhr.responseText;
  if (body && body.indexOf("data-tui-toast") !== -1) {
    document.body.insertAdjacentHTML("beforeend", body);
  }
});
