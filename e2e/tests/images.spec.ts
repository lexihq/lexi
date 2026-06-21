import { test, expect } from "./fixtures";
import { readFileSync } from "node:fs";

// The managed Images section over the local image store.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("manage local images: copy, publish, alias add/remove, delete", async ({ page }) => {
  // Image delete asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());
  await page.goto("/");
  await page.getByRole("link", { name: "Images" }).click();
  await expect(page).toHaveURL(/\/images$/);
  const table = page.locator("#images-table");
  await expect(table.getByText("debian/12")).toBeVisible();

  // Copy a catalog alias into the local store via the Copy-from-remote dialog.
  await page.getByRole("button", { name: "Copy from remote" }).click();
  await page.locator('form[hx-post="/images/copy"] input[name="alias"]').fill("alpine/edge");
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await expect(table.getByText("alpine/edge")).toBeVisible();

  // Publish the seeded (stopped) instance as an image with an alias.
  await page.getByRole("button", { name: "Publish from instance" }).click();
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

test("edit image details, export, and re-import", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/images");
  const table = page.locator("#images-table");
  const row = table.getByRole("row", { name: /debian\/12/ }).first();

  // Edit description + public via the row's details form.
  await expect(async () => {
    await row.getByText("Edit details").click();
    await row.locator('input[name="description"]').fill("edited by e2e");
    await row.getByRole("checkbox", { name: "Public" }).check();
    await row.getByRole("button", { name: "Save" }).click();
    await expect(table.getByText("edited by e2e").first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });
  // exact: true keeps this from matching the (hidden) "Public" checkbox label
  // inside other rows' closed details — getByText is case-insensitive without it.
  await expect(table.getByText("public", { exact: true }).first()).toBeVisible();

  // Export downloads a tarball named after the fingerprint.
  const downloadPromise = page.waitForEvent("download");
  await table.getByRole("row", { name: /edited by e2e/ }).first().getByRole("link", { name: "Export" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toMatch(/\.tar$/);

  // Re-import the downloaded blob with a fresh alias via the import dialog.
  const path = await download.path();
  await page.getByRole("button", { name: "Import from file" }).click();
  await page.locator('form[action="/images/import"] input[name="image"]').setInputFiles({
    name: "image.tar",
    mimeType: "application/octet-stream",
    buffer: readFileSync(path!),
  });
  await page.locator('form[action="/images/import"] input[name="alias"]').fill("e2e-restored");
  await page.getByRole("button", { name: "Import", exact: true }).click();
  await expect(page).toHaveURL(/\/images$/);
  await expect(table.getByText("e2e-restored")).toBeVisible();

  // Clean up the imported image (shared server state).
  await expect(async () => {
    await table.getByRole("row", { name: /e2e-restored/ }).getByRole("button", { name: "Delete" }).click();
    await expect(table.getByText("e2e-restored")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("export a split (VM) image as a zip and re-import it", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/images");
  const table = page.locator("#images-table");
  const vmRow = table.getByRole("row", { name: /Ubuntu Noble VM image/ }).first();
  await expect(vmRow).toBeVisible();

  // A split image downloads as a zip, not a tarball.
  const downloadPromise = page.waitForEvent("download");
  await vmRow.getByRole("link", { name: "Export" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toMatch(/\.zip$/);

  // Re-importing the zip restores a VM image under a fresh alias.
  const path = await download.path();
  await page.getByRole("button", { name: "Import from file" }).click();
  await page.locator('form[action="/images/import"] input[name="image"]').setInputFiles({
    name: "image.zip",
    mimeType: "application/octet-stream",
    buffer: readFileSync(path!),
  });
  await page.locator('form[action="/images/import"] input[name="alias"]').fill("e2e-vm-restored");
  await page.getByRole("button", { name: "Import", exact: true }).click();
  await expect(page).toHaveURL(/\/images$/);
  const restored = table.getByRole("row", { name: /e2e-vm-restored/ });
  await expect(restored).toBeVisible();
  await expect(restored.getByText("virtual-machine")).toBeVisible();

  // Clean up the imported image (shared server state).
  await expect(async () => {
    await restored.getByRole("button", { name: "Delete" }).click();
    await expect(table.getByText("e2e-vm-restored")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("image lifecycle: auto-update, expiry, and refresh on a copied image", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/images");
  const table = page.locator("#images-table");

  // Copy a catalog image so there is an update source to manage.
  await page.getByRole("button", { name: "Copy from remote" }).click();
  await page.locator('form[hx-post="/images/copy"] input[name="alias"]').fill("alpine/edge");
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  const row = table.getByRole("row", { name: /alpine\/edge/ });
  await expect(row).toBeVisible();

  // Enable auto-update with an expiry via Edit details.
  await row.getByText("Edit details").click();
  await row.locator('input[name="auto_update"]').check();
  await row.locator('input[name="expires_at"]').fill("2027-03-01T00:00");
  await row.getByRole("button", { name: "Save" }).click();
  // Exact match: the row also contains the edit form's "Auto-update" label,
  // and getByText's default substring match is case-insensitive.
  await expect(table.getByRole("row", { name: /alpine\/edge/ }).getByText("auto-update", { exact: true })).toBeVisible();
  await expect(table.getByRole("row", { name: /alpine\/edge/ }).getByText("expires 2027-03-01")).toBeVisible();

  // Refresh re-pulls from the source (fake logs the operation).
  await table.getByRole("row", { name: /alpine\/edge/ }).getByRole("button", { name: "Refresh" }).click();
  await expect(table.getByRole("row", { name: /alpine\/edge/ })).toBeVisible();

  // Clean up for reruns against a reused server.
  await expect(async () => {
    await table.getByRole("row", { name: /alpine\/edge/ }).getByRole("button", { name: "Delete" }).click();
    await expect(table.getByRole("row", { name: /alpine\/edge/ })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
