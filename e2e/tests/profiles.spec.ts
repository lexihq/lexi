import { test, expect } from "@playwright/test";

// Profiles: browse/attach, create/edit/rename/delete, and device editing.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("browse profiles and attach one to an instance", async ({ page }) => {
  // Profiles list + read-only detail.
  await page.goto("/profiles");
  await expect(page.getByRole("link", { name: "default" })).toBeVisible();
  await page.getByRole("link", { name: "gpu" }).click();
  await expect(page).toHaveURL(/\/profiles\/gpu$/);

  // Attach gpu to the seeded "demo" instance from its Summary tab.
  await page.goto("/instances/demo");
  const profiles = page.locator("#profiles");
  await profiles.getByRole("checkbox", { name: "gpu" }).check();
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/profiles"),
    ),
    profiles.getByRole("button", { name: "Apply profiles" }).click(),
  ]);
  // The swapped-in control keeps gpu checked.
  await expect(page.locator("#profiles").getByRole("checkbox", { name: "gpu" })).toBeChecked();
});

test("create, edit, and delete a profile", async ({ page }) => {
  await page.goto("/profiles");

  // Create.
  await page.locator('input[name="name"]').fill("e2e-prof");
  await page.locator('input[name="description"]').fill("made by e2e");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(/\/profiles\/e2e-prof$/);

  // Edit config via the trailing blank row.
  await page.locator('input[name="key"]').last().fill("limits.cpu");
  await page.locator('textarea[name="value"]').last().fill("2");
  await page.getByRole("button", { name: "Apply config" }).click();
  await expect(page).toHaveURL(/\/profiles\/e2e-prof$/);
  await expect(page.locator('input[name="key"][value="limits.cpu"]')).toBeVisible();

  // Add a nic device via the typed form; the #profile-devices section swaps in.
  const devices = page.locator("#profile-devices");
  const addNic = devices.locator('details:has-text("Add nic")');
  await addNic.locator("summary").click();
  await addNic.locator('input[name="device"]').fill("eth0");
  await addNic.locator('input[name="network"]').fill("incusbr0");
  await addNic.getByRole("button", { name: "Add nic" }).click();
  await expect(page.locator("#profile-devices").getByText("eth0", { exact: true })).toBeVisible();

  // Rename the profile; the page redirects to the new detail URL.
  await page.locator('input[name="new_name"]').fill("e2e-prof2");
  await page.getByRole("button", { name: "Rename" }).click();
  await expect(page).toHaveURL(/\/profiles\/e2e-prof2$/);
  await expect(page.locator("#profile-devices").getByText("eth0", { exact: true })).toBeVisible();

  // Delete.
  await page.getByRole("button", { name: "Delete" }).click();
  await expect(page).toHaveURL(/\/profiles$/);
  await expect(page.locator("table").getByText("e2e-prof", { exact: true })).toHaveCount(0);
  await expect(page.locator("table").getByText("e2e-prof2", { exact: true })).toHaveCount(0);
});

test("default profile keeps devices after a config edit and has no delete", async ({ page }) => {
  await page.goto("/profiles/default");
  await expect(page.getByRole("button", { name: "Delete" })).toHaveCount(0);
  await expect(page.getByText("eth0", { exact: true })).toBeVisible();

  await page.locator('input[name="key"]').last().fill("user.e2e-prof");
  await page.locator('textarea[name="value"]').last().fill("1");
  await page.getByRole("button", { name: "Apply config" }).click();

  await expect(page).toHaveURL(/\/profiles\/default$/);
  // Devices survived the config-only edit.
  await expect(page.getByText("eth0", { exact: true })).toBeVisible();
  await expect(page.getByText("root", { exact: true })).toBeVisible();
});
