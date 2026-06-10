import { test, expect } from "@playwright/test";

// Storage: custom volumes, volume snapshots, and pool create/delete.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("create and delete a custom volume in the Storage section", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Storage" }).click();
  await expect(page).toHaveURL(/\/storage$/);
  await page.getByRole("link", { name: "default" }).click();
  await expect(page).toHaveURL(/\/storage\/default$/);

  const volumes = page.locator("#volumes");
  await expect(volumes).toBeVisible();
  await volumes.locator('input[name="name"]').fill("e2e-vol");
  await page.getByRole("button", { name: "Create volume" }).click();

  await expect(page).toHaveURL(/\/storage\/default$/);
  await expect(page.locator("#volumes").getByText("e2e-vol")).toBeVisible();

  // Edit the volume: description + resize via the size key (versioned editor).
  await page.locator("#volumes").getByRole("link", { name: "e2e-vol" }).click();
  await expect(page).toHaveURL(/\/storage\/default\/volumes\/e2e-vol$/);
  const editor = page.locator('form[action="/storage/default/volumes/e2e-vol/config"]');
  await editor.locator('input[name="description"]').fill("resized by e2e");
  await editor.locator('input[name="key"]').last().fill("size");
  await editor.locator('textarea[name="value"]').last().fill("2GiB");
  await editor.getByRole("button", { name: "Apply config" }).click();
  await expect(page).toHaveURL(/\/storage\/default\/volumes\/e2e-vol$/);
  await expect(page.locator('input[name="key"][value="size"]')).toBeVisible();

  // Rename it; the page redirects to the new volume URL.
  await page.locator('input[name="new_name"]').fill("e2e-vol2");
  await page.getByRole("button", { name: "Rename" }).click();
  await expect(page).toHaveURL(/\/storage\/default\/volumes\/e2e-vol2$/);

  // Back on the pool page, delete the renamed volume. Same htmx
  // swap-then-click race as the snapshot/device Delete: retry until the delete
  // takes effect (a single lost click would otherwise fail).
  await page.goto("/storage/default");
  await expect(async () => {
    await page.locator("#volumes").getByRole("row", { name: /e2e-vol2/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volumes").getByText("e2e-vol2")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("snapshot a custom volume: create, restore, and delete", async ({ page }) => {
  await page.goto("/storage/default");
  // Distinct volume name so this doesn't collide with the volume-CRUD test on
  // the shared fake server.
  await page.locator("#volumes").locator('input[name="name"]').fill("e2e-snapvol");
  await page.getByRole("button", { name: "Create volume" }).click();
  await page.locator("#volumes").getByRole("link", { name: "e2e-snapvol" }).click();
  await expect(page).toHaveURL(/\/storage\/default\/volumes\/e2e-snapvol$/);

  const snaps = page.locator("#volume-snapshots");
  await expect(snaps).toBeVisible();
  await snaps.locator('input[name="snapshot"]').fill("snap0");
  await page.getByRole("button", { name: "Create snapshot" }).click();
  await expect(snaps).toContainText("snap0");

  await snaps.getByRole("button", { name: "Restore" }).click();
  await expect(snaps).toContainText("snap0");

  // Set an expiry; the Expires cell shows it in UTC.
  await expect(async () => {
    await page.locator("#volume-snapshots").locator('input[name="expires_at"]').fill("2031-05-06T07:08");
    await page.locator("#volume-snapshots").getByRole("button", { name: "Set expiry (UTC)" }).click();
    await expect(page.locator("#volume-snapshots")).toContainText("2031-05-06 07:08 UTC", { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Rename snap0 → snap1.
  await expect(async () => {
    await page.locator("#volume-snapshots").locator('input[name="new_name"]').fill("snap1");
    await page.locator("#volume-snapshots").getByRole("button", { name: "Rename" }).click();
    await expect(page.locator("#volume-snapshots").getByText("snap1")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // htmx swap-then-click race: retry the delete until it takes effect.
  await expect(async () => {
    await page.locator("#volume-snapshots").getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volume-snapshots").getByText("snap1")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Clean up the volume from the pool page (shared server state).
  await page.goto("/storage/default");
  await expect(async () => {
    await page.locator("#volumes").getByRole("row", { name: /e2e-snapvol/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volumes").getByText("e2e-snapvol")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("storage: create and delete a pool; in-use pool can't be deleted", async ({ page }) => {
  // The pool delete button asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());
  await page.goto("/storage");
  await page.getByRole("link", { name: "Create pool" }).click();
  await expect(page).toHaveURL(/\/storage\/new$/);
  await page.locator('input[name="name"]').fill("e2e-pool");
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page).toHaveURL(/\/storage$/);
  await expect(page.getByRole("link", { name: "e2e-pool" })).toBeVisible();

  // Edit the pool: set a description and a config key via the versioned
  // editor. Scope to the editor form — the volume-create form on the same page
  // also has key/value config rows.
  await page.getByRole("link", { name: "e2e-pool" }).click();
  const editor = page.locator('form[action="/storage/e2e-pool/config"]');
  await editor.locator('input[name="description"]').fill("edited by e2e");
  await editor.locator('input[name="key"]').last().fill("rsync.bwlimit");
  await editor.locator('textarea[name="value"]').last().fill("10MiB");
  await editor.getByRole("button", { name: "Apply config" }).click();
  await expect(page).toHaveURL(/\/storage\/e2e-pool$/);
  await expect(page.locator('input[name="key"][value="rsync.bwlimit"]')).toBeVisible();
  await expect(page.locator('input[name="description"]')).toHaveValue("edited by e2e");

  // Delete the (unused) pool from its detail page.
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page).toHaveURL(/\/storage$/);
  await expect(page.getByRole("link", { name: "e2e-pool" })).toHaveCount(0);

  // The seeded default pool is referenced by the default profile's root
  // device, so its delete button is disabled.
  await page.getByRole("link", { name: "default", exact: true }).click();
  await expect(page).toHaveURL(/\/storage\/default$/);
  await expect(page.getByRole("button", { name: "Delete", exact: true })).toBeDisabled();
});
