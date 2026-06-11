import { test, expect } from "@playwright/test";

// Projects: management page lifecycle and the sidebar switcher scoping the
// whole UI. Runs against the shared fake-backed server (instance "demo"
// seeded in the default project).

test("projects: create, switch scope, edit, rename, and delete", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  // Create a project from the management page (networks stays unchecked).
  await page.goto("/projects");
  await page.locator('input[name="name"]').fill("e2e-proj");
  await page.locator('input[name="description"]').fill("made by e2e");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(/\/projects\/e2e-proj$/);

  // The sidebar switcher appears once a second project exists; switching
  // scopes the instance list (demo lives in default). The select's submit can
  // be lost to the htmx settle race like any freshly-swapped control — retry
  // until the scope visibly changed (suite-wide pattern).
  const switchTo = async (name: string, settled: () => Promise<void>) => {
    await expect(async () => {
      await page.locator('select[name="project"]').selectOption(name);
      await settled();
    }).toPass({ timeout: 15000 });
  };
  await switchTo("e2e-proj", async () => {
    await expect(page.getByRole("link", { name: "demo" })).toHaveCount(0, { timeout: 1000 });
  });
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('select[name="project"]')).toHaveValue("e2e-proj");

  // Resources made while scoped land in the project: create an instance.
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill("e2e-proj-inst");
  await page.getByRole("button", { name: "Create instance" }).click();
  await expect(page.locator("#instance-e2e-proj-inst")).toContainText("e2e-proj-inst");

  // Switch back to default: the project's instance is invisible, demo is back.
  await switchTo("default", async () => {
    await expect(page.locator("#instance-e2e-proj-inst")).toHaveCount(0, { timeout: 1000 });
  });
  await expect(page.getByRole("link", { name: "demo" }).first()).toBeVisible();

  // A non-empty project's Delete is disabled with the reason.
  await page.goto("/projects/e2e-proj");
  await expect(page.getByRole("button", { name: "Delete", exact: true })).toBeDisabled();

  // Empty it (switch in, delete the instance), then rename and delete.
  await switchTo("e2e-proj", async () => {
    await expect(page.locator("#instance-e2e-proj-inst")).toBeVisible({ timeout: 1000 });
  });
  await expect(async () => {
    await page.locator("#instance-e2e-proj-inst").getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#instance-e2e-proj-inst")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  await page.goto("/projects/e2e-proj");
  await page.locator('input[name="new_name"]').fill("e2e-proj2");
  await page.getByRole("button", { name: "Rename" }).click();
  await expect(page).toHaveURL(/\/projects\/e2e-proj2$/);
  // The switcher followed the rename (cookie rewrite).
  await expect(page.locator('select[name="project"]')).toHaveValue("e2e-proj2");

  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page).toHaveURL(/\/projects$/);
  await expect(page.getByRole("link", { name: "e2e-proj2" })).toHaveCount(0);
  // Deleting the selected project fell back to default.
  await expect(page.getByRole("link", { name: "demo" }).first()).toBeVisible();
});
