import { test, expect } from "@playwright/test";

// The instance Configuration tab: key/value editing and multiline values.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("edit instance config in the Configuration tab", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Configuration" }).click();
  const config = page.locator("#config");
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
  await expect(page.locator('#config input[value="security.nesting"]')).toHaveCount(0);
});

test("config values support multiline (cloud-init)", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Configuration" }).click();
  const config = page.locator("#config");

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
  const row = page.locator('#config input[value="user.user-data"]');
  await expect(row).toBeVisible();
  await expect(
    page.locator("#config textarea", { hasText: "#cloud-config" }).first(),
  ).toHaveValue("#cloud-config\npackages:\n  - htop");
});
