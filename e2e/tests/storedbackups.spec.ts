import { test, expect } from "@playwright/test";

// Server-side named backups: the Backups tab on the instance detail page.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("stored backups: create, restore-as, and delete from the Backups tab", async ({ page }) => {
  // Backup delete asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());

  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Backups" }).click();
  const backups = page.locator("#backups");
  await expect(backups).toBeVisible();

  // Create a named instance-only backup.
  await backups.locator('input[name="name"]').fill("e2e-bk");
  await backups.locator('input[name="instance_only"]').check();
  await backups.getByRole("button", { name: "Create backup" }).click();
  await expect(backups.getByRole("cell", { name: "e2e-bk", exact: true })).toBeVisible();
  await expect(backups.getByRole("table").getByText("instance only")).toBeVisible();

  // Restore it under a new name; the redirect lands on the new instance.
  await backups.getByRole("button", { name: "Restore…" }).click();
  const dialog = page.locator("#restore-bk-e2e-bk");
  await expect(dialog).toBeVisible();
  await dialog.locator('input[name="new_name"]').fill("e2e-bk-restored");
  await dialog.getByRole("button", { name: "Restore", exact: true }).click();
  await expect(page).toHaveURL(/\/instances\/e2e-bk-restored$/);

  // Clean up: delete the restored instance, then the backup.
  await page.goto("/");
  const restored = page.locator("#instance-e2e-bk-restored");
  const deleteItem = restored.getByRole("menuitem", { name: "Delete", exact: true });
  await expect(async () => {
    if (!(await deleteItem.isVisible())) {
      await restored.getByRole("button", { name: "Actions for e2e-bk-restored" }).click();
    }
    await deleteItem.click();
    await expect(restored).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  await page.goto("/instances/demo?tab=backups");
  await expect(async () => {
    await page.locator("#backups").getByRole("button", { name: "Delete", exact: true }).click();
    await expect(page.locator("#backups").getByText("e2e-bk", { exact: true })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
  await expect(page.locator("#backups").getByText("No backups yet")).toBeVisible();
});
