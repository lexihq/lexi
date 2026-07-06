import { test, expect } from "@playwright/test";

// The instance Configuration tab: friendly Options toggles plus the raw
// key/value editor behind an "Advanced" disclosure, and multiline values.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("edit instance config in the Configuration tab", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Configuration" }).click();
  const config = page.locator("#config");
  // The raw editor lives behind the Advanced disclosure; each panel re-render
  // collapses it again, so reopen before touching the raw rows.
  const openAdvanced = () => config.getByText("Advanced: raw configuration").click();
  await openAdvanced();
  await expect(config.getByRole("button", { name: "Apply config" })).toBeVisible();

  // Add a key via the trailing blank row.
  await config.locator('input[name="key"]').last().fill("security.nesting");
  await config.locator('textarea[name="value"]').last().fill("true");
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/config"),
    ),
    config.getByRole("button", { name: "Apply config" }).click(),
  ]);
  // The Options toggle mirrors the raw key.
  await expect(config.locator('input[name="security.nesting"]')).toBeChecked();
  await openAdvanced();
  await expect(page.locator('#config input[value="security.nesting"]')).toBeVisible();
  // The save emits an out-of-band success toast without clobbering the panel.
  await expect(page.locator('[data-tui-toast][data-variant="success"]')).toBeVisible();

  // Remove it: clear the key and apply.
  await page.locator('#config input[value="security.nesting"]').fill("");
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/config"),
    ),
    page.locator("#config").getByRole("button", { name: "Apply config" }).click(),
  ]);
  await openAdvanced();
  await expect(page.locator('#config input[value="security.nesting"]')).toHaveCount(0);
});

test("options toggles merge without touching other keys", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Configuration" }).click();
  const config = page.locator("#config");

  // Flip autostart on via the friendly toggle.
  await config.locator('input[name="boot.autostart"]').check();
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/options"),
    ),
    config.getByRole("button", { name: "Apply options" }).click(),
  ]);
  await expect(config.locator('input[name="boot.autostart"]')).toBeChecked();
  // The raw editor shows the key the toggle wrote.
  await config.getByText("Advanced: raw configuration").click();
  await expect(config.locator('input[value="boot.autostart"]')).toBeVisible();

  // Flip it back off so the seeded state is restored for later specs.
  await config.locator('input[name="boot.autostart"]').uncheck();
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/options"),
    ),
    config.getByRole("button", { name: "Apply options" }).click(),
  ]);
  await expect(config.locator('input[name="boot.autostart"]')).not.toBeChecked();
});

test("config values support multiline (cloud-init)", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Configuration" }).click();
  const config = page.locator("#config");
  await config.getByText("Advanced: raw configuration").click();

  await config.locator('input[name="key"]').last().fill("user.user-data");
  await config
    .locator('textarea[name="value"]')
    .last()
    .fill("#cloud-config\npackages:\n  - htop");
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/config"),
    ),
    config.getByRole("button", { name: "Apply config" }).click(),
  ]);

  // The re-rendered panel keeps all three lines in the value textarea.
  await config.getByText("Advanced: raw configuration").click();
  const row = page.locator('#config input[value="user.user-data"]');
  await expect(row).toBeVisible();
  await expect(
    page.locator("#config textarea", { hasText: "#cloud-config" }).first(),
  ).toHaveValue("#cloud-config\npackages:\n  - htop");
});
