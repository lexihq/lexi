import { test, expect } from "./fixtures";

// Bulk actions on the instances list: multi-select via the checkbox column and
// the bulk-action bar. Runs against the shared fake-backed server (instance
// "demo" seeded). Selection correctness across many instances is unit-tested;
// this exercises the browser wiring (checkbox → bar → htmx post → swap).

test("bulk actions: select-all reveals the bar and a bulk action runs on the selection", async ({ page }) => {
  await page.goto("/");

  const bar = page.locator("[data-bulk-bar]");
  await expect(bar).toBeHidden();

  // The header select-all checks every row and reveals the bar with a count.
  await page.locator("[data-bulk-all]").check();
  await expect(bar).toBeVisible();
  await expect(page.locator("[data-bulk-count]")).toContainText("selected");
  await expect(page.locator('[data-bulk-cb][value="demo"]')).toBeChecked();

  // Bulk Start acts on the selection; the table re-renders and the bar resets
  // (the swapped-in checkboxes are unchecked).
  await bar.getByRole("button", { name: "Start", exact: true }).click();
  await expect(page.locator("#instance-demo")).toContainText("Running");
  await expect(bar).toBeHidden();
  // The bulk op affirms with a summary toast.
  await expect(page.locator('[data-tui-toast][data-variant="success"]')).toBeVisible();

  // Restore the seeded Stopped state (shared server) and assert the reverse
  // bulk transition while we're here.
  await page.locator('[data-bulk-cb][value="demo"]').check();
  await bar.getByRole("button", { name: "Stop", exact: true }).click();
  await expect(page.locator("#instance-demo")).toContainText("Stopped");
});

test("bulk actions: deselecting the last row hides the bar again", async ({ page }) => {
  await page.goto("/");
  const box = page.locator('[data-bulk-cb][value="demo"]');
  const bar = page.locator("[data-bulk-bar]");

  await box.check();
  await expect(bar).toBeVisible();
  await box.uncheck();
  await expect(bar).toBeHidden();
});
