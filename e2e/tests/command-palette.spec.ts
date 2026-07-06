import { test, expect } from "@playwright/test";

// The ⌘K / Ctrl-K command palette: a quick switcher that scrapes the sidebar's
// nav + instance links so it always matches the live navigation.
// Runs against the shared fake-backed server (instance "demo" seeded).

test("command palette: shortcut opens it, filters, and jumps to an instance", async ({ page }) => {
  await page.goto("/");
  const dialog = page.locator("#command-palette");
  await expect(dialog).toBeHidden();

  // The JS accepts either modifier; ControlOrMeta covers both platforms.
  await page.keyboard.press("ControlOrMeta+k");
  await expect(dialog).toBeVisible();
  await expect(page.locator("#cmdk-input")).toBeFocused();

  // It lists pages, the seeded instance, and its state-appropriate actions
  // (demo is stopped, so a Start action).
  const results = page.locator("#cmdk-results [role=option]");
  await expect(results.filter({ hasText: "Instances" }).first()).toBeVisible();
  await expect(results.filter({ hasText: /^demoInstance$/ })).toBeVisible();
  await expect(results.filter({ hasText: "Start demo" })).toBeVisible();

  // Filtering narrows to the instance and its actions; the top match is the
  // instance itself, and Enter jumps to it.
  await page.locator("#cmdk-input").fill("demo");
  await expect(results.first()).toContainText("demo");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/instances\/demo$/);
  await expect(dialog).toBeHidden();
});

test("command palette: the header trigger opens it and Escape closes it", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Search instances and pages" }).click();
  const dialog = page.locator("#command-palette");
  await expect(dialog).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
});
