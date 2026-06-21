import { test as base } from "@playwright/test";

export { expect, type Page, type Dialog } from "@playwright/test";

// Destructive actions used to prompt via the browser's window.confirm, which
// specs accepted with page.on("dialog", d => d.accept()). They now route through
// the app-styled #confirm-dialog (confirm-dialog.js intercepts htmx:confirm), so
// no native dialog fires. This auto fixture installs an observer that clicks the
// dialog's accept button whenever it opens — the same "always accept" behaviour,
// for every test, without touching each call site. (The remaining page.on
// handlers are harmless no-ops kept as intent documentation.)
export const test = base.extend<{ autoAcceptConfirms: void }>({
  autoAcceptConfirms: [
    async ({ page }, use) => {
      await page.addInitScript(() => {
        const accept = () => {
          const d = document.getElementById("confirm-dialog") as HTMLDialogElement | null;
          if (d && d.open) {
            (document.getElementById("confirm-dialog-accept") as HTMLButtonElement | null)?.click();
          }
        };
        const start = () =>
          new MutationObserver(accept).observe(document.documentElement, {
            subtree: true,
            attributes: true,
            attributeFilter: ["open"],
          });
        if (document.readyState === "loading") {
          document.addEventListener("DOMContentLoaded", start);
        } else {
          start();
        }
      });
      await use();
    },
    { auto: true },
  ],
});
