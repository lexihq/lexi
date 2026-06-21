import { test, expect } from "./fixtures";

// Server-side volume backups: the Backups section on the custom-volume page.
// Runs against the shared fake-backed server (the "default" pool exists).

test("volume backups: create, download, restore-as, and delete", async ({ page }) => {
  // Backup delete asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());

  // Create a volume to back up.
  await page.goto("/storage/default");
  await page.locator("#volumes").locator('input[name="name"]').fill("e2e-vbkvol");
  await page.getByRole("button", { name: "Create volume" }).click();
  await page.locator("#volumes").getByRole("link", { name: "e2e-vbkvol" }).click();

  const backups = page.locator("#volume-backups");
  await expect(backups).toBeVisible();

  // Create a named volume-only backup.
  await backups.locator('input[name="name"]').fill("e2e-vbk");
  await backups.locator('input[name="volume_only"]').check();
  await backups.getByRole("button", { name: "Create backup" }).click();
  await expect(backups.getByRole("cell", { name: "e2e-vbk", exact: true })).toBeVisible();
  await expect(backups.getByRole("table").getByText("volume only")).toBeVisible();

  // Download streams a tarball.
  const downloadPromise = page.waitForEvent("download");
  await backups.getByRole("link", { name: "Download" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toContain("e2e-vbkvol-e2e-vbk");

  // Restore it as a new volume; the redirect lands on the new volume page.
  await backups.getByRole("button", { name: "Restore…" }).click();
  const dialog = page.locator("#restore-vbk-e2e-vbk");
  await expect(dialog).toBeVisible();
  await dialog.locator('input[name="name"]').fill("e2e-vbk-restored");
  await dialog.getByRole("button", { name: "Restore", exact: true }).click();
  await expect(page).toHaveURL(/\/storage\/default\/volumes\/e2e-vbk-restored$/);

  // Clean up: delete the restored volume.
  await page.goto("/storage/default");
  await expect(async () => {
    await page.locator("#volumes").getByRole("row", { name: /e2e-vbk-restored/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volumes").getByText("e2e-vbk-restored", { exact: true })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Delete the backup.
  await page.goto("/storage/default/volumes/e2e-vbkvol");
  await expect(async () => {
    await page.locator("#volume-backups").getByRole("button", { name: "Delete", exact: true }).click();
    await expect(page.locator("#volume-backups").getByText("e2e-vbk", { exact: true })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
  await expect(page.locator("#volume-backups").getByText("No backups yet")).toBeVisible();
});
