// The typed-name delete gate, tested WITHOUT the shared auto-accept fixture:
// that fixture fills the dialog input with the correct name, so every other
// spec passes *through* the gate without ever testing it. This file imports
// the raw Playwright test so a gate stuck open (accept enabled without the
// exact name) fails loudly instead of silently removing the safety feature.
import { test, expect } from "@playwright/test";

test("typed-name delete gate arms only on the exact name", async ({ page }) => {
  await page.goto("/");
  const row = page.locator("#instance-demo");
  await row.getByRole("button", { name: "Actions for demo" }).click();
  await row.getByRole("menuitem", { name: "Delete", exact: true }).click();

  const dialog = page.locator("#confirm-dialog");
  await expect(dialog).toHaveAttribute("open", "");
  const accept = page.locator("#confirm-dialog-accept");
  const input = page.locator("#confirm-dialog-input");
  await expect(input).toBeVisible();

  await expect(accept).toBeDisabled();
  await input.fill("demo2");
  await expect(accept).toBeDisabled();
  await input.fill("demo");
  await expect(accept).toBeEnabled();

  // Leave the seeded instance alive for the rest of the suite.
  await page.locator("#confirm-dialog-cancel").click();
  await expect(dialog).not.toHaveAttribute("open", "");
  await expect(page.locator("#instance-demo")).toBeVisible();
});

test("the server rejects a delete without the typed confirmation", async ({ page }) => {
  const missing = await page.request.post("/instances/demo/delete", { form: {} });
  expect(missing.status()).toBe(400);

  const mistyped = await page.request.post("/instances/demo/delete", {
    form: { confirm: "demo2" },
  });
  expect(mistyped.status()).toBe(400);

  await page.goto("/instances/demo");
  await expect(page.getByRole("heading", { name: "demo" })).toBeVisible();
});
