import { test, expect } from "@playwright/test";

// Networks: create/delete, managed-network editing, and error toasts.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("create and delete a network in the Networks section", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Networks" }).click();
  await expect(page).toHaveURL(/\/networks$/);
  await expect(page.getByText("incusbr0")).toBeVisible();

  await page.getByRole("link", { name: "Create network" }).click();
  await page.locator('input[name="name"]').fill("e2e-net");
  await page.locator('input[name="key"]').first().fill("ipv4.nat");
  await page.locator('input[name="value"]').first().fill("true");
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page).toHaveURL(/\/networks$/);
  const table = page.locator("#networks-table");
  await expect(table.getByText("e2e-net")).toBeVisible();

  await table.getByRole("row", { name: /e2e-net/ }).getByRole("button", { name: "Delete" }).click();
  await expect(page.locator("#networks-table").getByText("e2e-net")).toHaveCount(0);
});

test("edit a managed network's description and config", async ({ page }) => {
  await page.goto("/networks/incusbr0");
  await page.locator('input[name="description"]').fill("edited by e2e");
  // Blank key rows are appended after the existing config rows.
  await page.locator('input[name="key"]').last().fill("user.e2e");
  await page.locator('input[name="value"]').last().fill("yes");
  await page.getByRole("button", { name: "Apply config" }).click();

  await expect(page).toHaveURL(/\/networks\/incusbr0$/);
  await expect(page.locator('input[name="description"]')).toHaveValue("edited by e2e");
  await expect(page.locator('input[name="key"][value="user.e2e"]')).toBeVisible();
});

test("backend errors surface as a toast", async ({ page }) => {
  await page.goto("/networks/new");
  // incusbr0 is seeded → creating it again conflicts (409).
  await page.locator('input[name="name"]').fill("incusbr0");
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page.locator("[data-tui-toast]")).toBeVisible();
  // The form is not replaced by the error response, and the failed boosted
  // request must not rewrite history away from the create page.
  await expect(page.locator('input[name="name"]')).toBeVisible();
  await expect(page).toHaveURL(/\/networks\/new$/);
});
