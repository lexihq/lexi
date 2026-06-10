import { test, expect } from "@playwright/test";

// The instance Devices tab: typed add forms, in-place edit, and removal.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("add and remove a proxy device in the Devices tab", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Devices" }).click();
  const devices = page.locator("#devices");
  await expect(devices).toBeVisible();

  // Open the proxy add form and fill it.
  const proxyForm = devices.locator('details:has-text("Add proxy")');
  await proxyForm.locator("summary").click();
  await proxyForm.locator('input[name="device"]').fill("web");
  await proxyForm.locator('input[name="listen"]').fill("tcp:0.0.0.0:8080");
  await proxyForm.locator('input[name="connect"]').fill("tcp:127.0.0.1:80");
  await proxyForm.getByRole("button", { name: "Add proxy" }).click();
  await expect(devices.getByText("web", { exact: true })).toBeVisible();

  // Remove it via the Remove button on the local device row. htmx wires the
  // freshly-swapped-in button a tick after it renders, so a single click can be
  // lost; retry until the delete actually takes effect.
  await expect(async () => {
    await devices.getByRole("button", { name: "Remove" }).click();
    await expect(devices.getByText("web", { exact: true })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("edit a device in place in the Devices tab", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Devices" }).click();
  const devices = page.locator("#devices");

  // Add a proxy device to edit.
  const addForm = devices.locator("details", { hasText: "Add proxy" });
  await addForm.locator("summary").click();
  await addForm.locator('input[name="device"]').fill("edit-me");
  await addForm.locator('input[name="listen"]').fill("tcp:0.0.0.0:80");
  await addForm.locator('input[name="connect"]').fill("tcp:127.0.0.1:80");
  await addForm.getByRole("button", { name: "Add proxy" }).click();
  await expect(devices.getByText("edit-me", { exact: true })).toBeVisible();

  // Edit it: new listen, blank connect (= remove the key).
  const editForm = devices.locator("details", { hasText: "Edit" }).first();
  await editForm.locator("summary").click();
  await editForm.locator('input[name="listen"]').fill("tcp:0.0.0.0:9090");
  await editForm.locator('input[name="connect"]').fill("");
  await editForm.getByRole("button", { name: "Save" }).click();

  await expect(devices.getByText("tcp:0.0.0.0:9090")).toBeVisible();
  await expect(devices.getByText("tcp:127.0.0.1:80")).toHaveCount(0);

  // Cleanup so other tests see a pristine demo instance. Same htmx race as the
  // add/remove test above: the swapped-in Remove button is wired a tick after
  // it renders, so a single click can be lost; retry until the delete sticks.
  await expect(async () => {
    await devices.getByRole("button", { name: "Remove" }).click();
    await expect(devices.getByText("edit-me", { exact: true })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
