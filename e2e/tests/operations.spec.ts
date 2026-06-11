import { test, expect } from "@playwright/test";

// The bottom Tasks panel: operation listing, SSE push updates, and cancel.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("tasks panel lists operations and picks up new ones", async ({ page }) => {
  const name = "e2e-task";

  // Creating an instance records an operation in the fake's task log.
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill(name);
  await page.getByRole("button", { name: "Create instance" }).click();
  const row = page.locator(`#instance-${name}`);
  await expect(row).toBeVisible();

  // Expand the bottom Tasks panel; the fake backend is Events-capable, so the
  // panel is SSE-wired and painted by the stream's initial frame.
  const footer = page.locator("footer");
  await expect(footer.locator('[sse-connect="/events/operations"]')).toBeAttached();
  await footer.locator('label[for="ops-toggle"]').click();
  await expect(footer.getByText(`Creating instance "${name}"`)).toBeVisible();

  // Delete the instance via the row's kebab menu (accepting the hx-confirm
  // dialog); the SSE push delivers the new operation.
  const deleteItem = row.getByRole("menuitem", { name: "Delete", exact: true });
  // One persistent handler for the whole retry loop: stacking a page.once per
  // attempt would invoke several accepts on the same dialog, which throws.
  page.on("dialog", (d) => void d.accept().catch(() => {}));
  await expect(async () => {
    if (!(await deleteItem.isVisible())) {
      await row.getByRole("button", { name: `Actions for ${name}` }).click();
    }
    await deleteItem.click();
    await expect(row).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
  // 3s would be unreachable for the old 5s poll in the worst case; SSE pushes
  // the frame within milliseconds of the delete.
  await expect(footer.getByText(`Deleting instance "${name}"`)).toBeVisible({ timeout: 3000 });
});

test("tasks panel: cancel a running operation", async ({ page }) => {
  await page.goto("/instances/demo");
  const footer = page.locator("footer");
  await footer.locator('label[for="ops-toggle"]').click();

  // The fakeserver seeds a cancelable "Migrating instance" task. Cancel it (if a
  // prior run on a reused server already did, it stays Cancelled with no button).
  const ops = page.locator("#operations");
  await expect(ops.getByText('Migrating instance "demo"')).toBeVisible({ timeout: 10000 });
  await expect(async () => {
    const cancel = ops.getByRole("button", { name: "Cancel" });
    if (await cancel.count()) {
      await cancel.first().click();
    }
    await expect(ops.getByText("Cancelled")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
