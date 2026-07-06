// Surface backend errors (4xx/5xx) from htmx requests as a toast. HTMX 2 skips
// swapping non-2xx responses by default, so without this every mutating form
// silently swallows backend errors.
//
// We deliberately leave evt.detail.shouldSwap at its default (false): forcing the
// swap would (a) clobber the targeted table/form and (b) for a boosted request
// run its normal history update, corrupting the URL (e.g. a failed create on
// /networks/new would push /networks while the form stays). Instead we render the
// toast ourselves by appending it to <body>, where toast.js's MutationObserver
// initializes it. Responses that are already toast markup are inserted as-is;
// anything else (the plain-text http.Error paths — daemon down, middleware
// failures) is wrapped in a text-only toast via textContent, so the failure is
// never silently invisible and non-HTML bodies can't inject markup.
document.body.addEventListener("htmx:beforeSwap", function (evt) {
  const xhr = evt.detail.xhr;
  if (!xhr || xhr.status < 400) return;
  const body = xhr.responseText;
  if (body && body.indexOf("data-tui-toast") !== -1) {
    document.body.insertAdjacentHTML("beforeend", body);
    return;
  }
  const message =
    (body || "").trim().slice(0, 300) || "request failed (" + xhr.status + ")";
  document.body.appendChild(textErrorToast(message));
});

// textErrorToast builds the same structure as the server's ErrorToast (see
// internal/components/toast) but with the message set via textContent.
function textErrorToast(message) {
  const toast = document.createElement("div");
  toast.setAttribute("data-tui-toast", "");
  toast.setAttribute("data-tui-toast-duration", "6000");
  toast.setAttribute("data-position", "top-right");
  toast.setAttribute("data-variant", "error");
  toast.setAttribute("role", "alert");
  toast.setAttribute("aria-live", "assertive");
  toast.className =
    "z-50 fixed pointer-events-auto p-4 w-full md:max-w-[420px] " +
    "animate-in fade-in duration-300 " +
    "data-[position=top-right]:top-0 data-[position=top-right]:right-0 " +
    "data-[position*=top]:slide-in-from-top-4";

  const card = document.createElement("div");
  card.className =
    "w-full bg-popover text-popover-foreground rounded-lg shadow-xs border " +
    "pt-5 pb-4 px-4 flex items-center justify-center relative overflow-hidden group";

  const content = document.createElement("span");
  content.className = "flex-1 min-w-0";
  const text = document.createElement("p");
  text.className = "text-sm opacity-90 mt-1";
  text.textContent = message;
  content.appendChild(text);
  card.appendChild(content);

  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.setAttribute("data-tui-toast-dismiss", "");
  dismiss.setAttribute("aria-label", "Dismiss");
  dismiss.className = "ml-3 flex-shrink-0 text-sm opacity-70 hover:opacity-100";
  dismiss.textContent = "✕";
  card.appendChild(dismiss);

  toast.appendChild(card);
  return toast;
}
