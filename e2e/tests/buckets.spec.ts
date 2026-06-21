import { test, expect } from "./fixtures";

// Storage buckets live on the pool detail page. The fake seeds the admin key
// on bucket creation, mirroring the daemon.

test("storage buckets: create, manage keys, and delete", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/storage/default");

  // Scope to main: the collapsed Tasks footer echoes names in operation rows.
  const main = page.locator("main");
  const buckets = main.locator("section", { has: page.getByRole("heading", { name: "Buckets" }) });
  await expect(buckets).toBeVisible();

  // Create a bucket with a quota; it appears with its S3 URL and admin key.
  await buckets.locator('form[action="/storage/default/buckets"] input[name="name"]').fill("e2e-bucket");
  await buckets.locator('form[action="/storage/default/buckets"] input[name="size"]').fill("100MiB");
  await buckets.getByRole("button", { name: "Create bucket" }).click();
  await expect(page).toHaveURL(/\/storage\/default$/);
  await expect(buckets.getByText("https://fake-s3:8555/e2e-bucket")).toBeVisible();
  await expect(buckets.getByText("100MiB").first()).toBeVisible();
  // The seeded admin key's row (name and role cells both read "admin").
  await expect(buckets.getByRole("row", { name: /admin.*FAKEACCESS/ })).toBeVisible();

  // Add a read-only key; its generated access key shows in the table and the
  // secret hides behind a reveal.
  const keyForm = buckets.locator('form[action="/storage/default/buckets/e2e-bucket/keys"]');
  await keyForm.locator('input[name="name"]').fill("ci");
  await keyForm.locator('select[name="role"]').selectOption("read-only");
  await keyForm.getByRole("button", { name: "Add key" }).click();
  const ciRow = buckets.getByRole("row", { name: /ci/ });
  await expect(ciRow.getByText("read-only")).toBeVisible();
  await expect(ciRow.getByText(/FAKEACCESS/)).toBeVisible();
  await ciRow.getByText("Reveal").click();
  await expect(ciRow.getByText(/fakesecret/)).toBeVisible();

  // Revoke the key, then delete the bucket.
  await expect(async () => {
    await ciRow.getByRole("button", { name: "Revoke" }).click();
    await expect(buckets.getByRole("row", { name: /ci/ })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  await expect(async () => {
    await buckets.getByRole("button", { name: "Delete", exact: true }).first().click();
    await expect(buckets.getByText("https://fake-s3:8555/e2e-bucket")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
