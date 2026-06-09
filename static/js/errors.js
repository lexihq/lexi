// Surface backend errors (4xx/5xx) from htmx requests as a toast. HTMX 2 skips
// swapping non-2xx responses by default, so without this every mutating form
// silently swallows backend errors. We force the swap and route the response
// (toast markup from server.renderError) to <body> via append, leaving the
// originally-targeted element (table/form) intact; toast.js then initializes it.
document.body.addEventListener("htmx:beforeSwap", function (evt) {
  if (evt.detail.xhr && evt.detail.xhr.status >= 400) {
    evt.detail.shouldSwap = true;
    evt.detail.target = document.body;
    evt.detail.swapOverride = "beforeend";
  }
});
