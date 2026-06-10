import { test, expect } from "@playwright/test";

// Instance snapshots: create/restore/delete, stateful flag, expiry, rename,
// and the auto-snapshot schedule form.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("snapshot create, restore, and delete on the detail page", async ({ page }) => {
  await page.goto("/instances/demo");
  // Snapshots live behind their tab; open it before driving the table.
  await page.getByRole("link", { name: "Snapshots" }).click();
  const snap = "e2e-snap";
  const snapshots = page.locator("#snapshots");
  await expect(snapshots).toBeVisible();

  await snapshots.locator("input[name=snapshot]").fill(snap);
  await page.getByRole("button", { name: "Create snapshot" }).click();
  await expect(snapshots).toContainText(snap);

  await snapshots.getByRole("button", { name: "Restore" }).click();
  await expect(snapshots).toContainText(snap);

  // Same htmx swap-then-click race as the device Remove: retry until the delete
  // takes effect (a single lost click would otherwise fail the assertion).
  await expect(async () => {
    await snapshots.getByRole("button", { name: "Delete" }).click();
    await expect(snapshots).not.toContainText(snap, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("snapshot stateful flag, expiry, and rename on the detail page", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Snapshots" }).click();
  const snapshots = page.locator("#snapshots");
  await expect(snapshots).toBeVisible();

  // Create a stateful snapshot with an expiry (the fake records the flag as-is).
  await snapshots.locator('input[name="snapshot"]').fill("e2e-state");
  await snapshots.locator('input[name="expires_at"]').first().fill("2026-06-01T00:00");
  await snapshots.locator('input[name="stateful"]').check();
  await page.getByRole("button", { name: "Create snapshot" }).click();

  const body = page.locator("#snapshots tbody");
  await expect(body.getByText("e2e-state")).toBeVisible();
  await expect(body.getByText("stateful", { exact: true })).toBeVisible();

  // Rename it (htmx swap-then-click retry).
  await expect(async () => {
    const row = page.locator("#snapshots tbody").getByRole("row").filter({ hasText: "e2e-state" });
    await row.locator('input[name="new_name"]').fill("e2e-state2");
    await row.getByRole("button", { name: "Rename" }).click();
    await expect(page.locator("#snapshots tbody").getByText("e2e-state2")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Clean up: delete (retry).
  await expect(async () => {
    await page.locator("#snapshots tbody").getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#snapshots tbody").getByText("e2e-state2")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("set an instance snapshot schedule", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Snapshots" }).click();

  // The schedule form lazy-loads into #snapshot-schedule; wait for its inputs.
  const schedule = page.locator('#snapshot-schedule input[name="schedule"]');
  await expect(schedule).toBeVisible();
  await schedule.fill("@daily");
  await page.locator('#snapshot-schedule input[name="expiry"]').fill("2w");
  await page.locator('#snapshot-schedule input[name="pattern"]').fill("snap%d");
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/snapshots/schedule"),
    ),
    page.getByRole("button", { name: "Save schedule" }).click(),
  ]);

  // Re-open the tab so the form re-fetches from the backend — this verifies the
  // values were persisted, not just that our typed-in inputs are still present.
  await page.getByRole("link", { name: "Summary" }).click();
  await page.getByRole("link", { name: "Snapshots" }).click();
  await expect(page.locator('#snapshot-schedule input[name="schedule"]')).toHaveValue("@daily");
  await expect(page.locator('#snapshot-schedule input[name="expiry"]')).toHaveValue("2w");
  await expect(page.locator('#snapshot-schedule input[name="pattern"]')).toHaveValue("snap%d");
});
