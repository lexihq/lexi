import { test, expect } from "@playwright/test";

// The instance Files tab: browse/upload/download, mkdir/delete, the text
// editor, and the read-only viewer.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("files tab: browse, download, and upload", async ({ page }) => {
  await page.goto("/instances/demo?tab=files");
  const files = page.locator("#files");
  await expect(files).toBeVisible();

  // Descend from the root into /etc. The panel content is freshly hx-loaded,
  // so retry a lost click (swap-then-click race).
  await expect(async () => {
    await files.getByRole("button", { name: "etc" }).click();
    await expect(files.getByText("hostname")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Download /etc/hostname (first file row alphabetically) and check the name.
  const downloadPromise = page.waitForEvent("download");
  await files.getByRole("link", { name: "Download" }).first().click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("hostname");

  // Upload a file into /etc and see its row appear.
  await files.locator('input[type="file"]').setInputFiles({
    name: "e2e-upload.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("hello from e2e"),
  });
  await expect(async () => {
    await files.getByRole("button", { name: "Upload" }).click();
    await expect(files.getByText("e2e-upload.txt")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("files tab: create folder, delete file and folder", async ({ page }) => {
  // Delete buttons ask via hx-confirm; accept every dialog.
  page.on("dialog", (d) => d.accept());
  await page.goto("/instances/demo?tab=files");
  const files = page.locator("#files");
  await expect(files).toBeVisible();

  // Create a folder at the root and see its row appear. Freshly swapped-in
  // panels can lose a click (htmx wires them a tick later), so retry.
  await expect(async () => {
    await files.locator('input[name="name"]').fill("e2e-dir");
    await files.getByRole("button", { name: "New folder" }).click();
    await expect(files.getByRole("button", { name: "e2e-dir" })).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Enter it (empty) and upload a file into it.
  await expect(async () => {
    await files.getByRole("button", { name: "e2e-dir" }).click();
    await expect(files.getByText("Empty directory.")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });
  await files.locator('input[type="file"]').setInputFiles({
    name: "inner.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("inner"),
  });
  await expect(async () => {
    await files.getByRole("button", { name: "Upload" }).click();
    await expect(files.getByText("inner.txt")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Delete the file; the panel re-renders in the same directory.
  await expect(async () => {
    await files.getByRole("button", { name: "Delete" }).click();
    await expect(files.getByText("inner.txt")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Go up and delete the now-empty folder.
  await expect(async () => {
    await files.getByRole("button", { name: "..", exact: true }).click();
    await expect(files.getByRole("button", { name: "e2e-dir" })).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });
  await expect(async () => {
    await files
      .getByRole("row", { name: /e2e-dir/ })
      .getByRole("button", { name: "Delete" })
      .click();
    await expect(files.getByRole("button", { name: "e2e-dir" })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("files tab: edit a text file in the browser", async ({ page }) => {
  await page.goto("/instances/demo?tab=files");
  const files = page.locator("#files");
  await expect(files).toBeVisible();
  await expect(async () => {
    await files.getByRole("button", { name: "etc" }).click();
    await expect(files.getByText("hostname")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Open the editor for /etc/hostname.
  await files
    .getByRole("row", { name: /hostname/ })
    .getByRole("link", { name: "Edit" })
    .click();
  await expect(page).toHaveURL(/\/files\/edit\?path=%2Fetc%2Fhostname/);
  const textarea = page.locator('textarea[name="content"]');
  // Don't assert the seeded value: with reuseExistingServer a previous run's
  // edit may still be in place. Non-empty proves the read path.
  await expect(textarea).not.toHaveValue("");

  // Save new content and land back on the Files tab.
  await textarea.fill("edited-by-e2e\n");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page).toHaveURL(/tab=files/);

  // Re-open: the edit persisted. Then restore the seeded content via a second
  // save so reruns against a reused server see a pristine file.
  await page.goto("/instances/demo/files/edit?path=%2Fetc%2Fhostname");
  await expect(page.locator('textarea[name="content"]')).toHaveValue("edited-by-e2e\n");
  await page.locator('textarea[name="content"]').fill("demo\n");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page).toHaveURL(/tab=files/);

  // The seeded binary file is refused with a clear message.
  const res = await page.request.get("/instances/demo/files/edit?path=%2Froot%2Fblob.bin");
  expect(res.status()).toBe(400);
  expect(await res.text()).toContain("binary file");
});

test("files tab: view a log the editor refuses", async ({ page }) => {
  await page.goto("/instances/demo?tab=files");
  const files = page.locator("#files");
  await expect(files).toBeVisible();
  await expect(async () => {
    await files.getByRole("button", { name: "root" }).click();
    await expect(files.getByText("app.log")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // The editor refuses the log's control bytes...
  const edit = await page.request.get("/instances/demo/files/edit?path=%2Froot%2Fapp.log");
  expect(edit.status()).toBe(400);

  // ...but the read-only viewer renders it.
  await files
    .getByRole("row", { name: /app\.log/ })
    .getByRole("link", { name: "View" })
    .click();
  await expect(page).toHaveURL(/\/files\/view\?path=%2Froot%2Fapp\.log/);
  await expect(page.getByText("boot ok")).toBeVisible();
  await expect(page.getByText("listening")).toBeVisible();
});
