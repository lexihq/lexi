import { test, expect } from "@playwright/test";

// The managed Images section over the local image store.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("manage local images: copy, publish, alias add/remove, delete", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Images" }).click();
  await expect(page).toHaveURL(/\/images$/);
  const table = page.locator("#images-table");
  await expect(table.getByText("debian/12")).toBeVisible();

  // Copy a catalog alias into the local store. The copy/publish forms live
  // outside the swapped table, so they have no swap-then-click race.
  await page.locator('form[hx-post="/images/copy"] input[name="alias"]').fill("alpine/edge");
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await expect(table.getByText("alpine/edge")).toBeVisible();

  // Publish the seeded (stopped) instance as an image with an alias.
  await page.locator('form[hx-post="/images/publish"] select[name="instance"]').selectOption("demo");
  await page.locator('form[hx-post="/images/publish"] input[name="alias"]').fill("e2e-pub");
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(table.getByText("e2e-pub")).toBeVisible();

  // Add an alias on the published row. Row controls live in freshly-swapped
  // content, so retry lost clicks (the usual swap-then-click race); a retry
  // after a successful add just 409s without changing the table.
  await expect(async () => {
    const row = table.getByRole("row", { name: /e2e-pub/ });
    await row.locator('input[name="alias"]').fill("e2e-extra");
    await row.locator('button[title="Add alias"]').click();
    await expect(table.getByText("e2e-extra")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Remove that alias via its chip button.
  await expect(async () => {
    await page.locator('button[title="Remove alias e2e-extra"]').click();
    await expect(table.getByText("e2e-extra")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Delete the published image.
  await expect(async () => {
    await table.getByRole("row", { name: /e2e-pub/ }).getByRole("button", { name: "Delete" }).click();
    await expect(table.getByText("e2e-pub")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
