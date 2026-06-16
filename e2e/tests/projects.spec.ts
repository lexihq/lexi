import { test, expect } from "@playwright/test";

// Projects: management page lifecycle and the sidebar switcher scoping the
// whole UI. Runs against the shared fake-backed server (instance "demo"
// seeded in the default project).

test("projects: create, switch scope, edit, rename, and delete", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  // Create a project from the management page (networks stays unchecked).
  await page.goto("/projects");
  await page.getByRole("button", { name: "Create project" }).click();
  await page.locator('input[name="name"]').fill("e2e-proj");
  await page.locator('input[name="description"]').fill("made by e2e");
  await page.getByRole("button", { name: "Create", exact: true }).click();
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
  // A fresh project has no instances; the list says so instead of rendering bare headers.
  await expect(page.getByText("No instances yet")).toBeVisible();

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
  const projInst = page.locator("#instance-e2e-proj-inst");
  const projInstDelete = projInst.getByRole("menuitem", { name: "Delete", exact: true });
  await expect(async () => {
    if (!(await projInstDelete.isVisible())) {
      await projInst.getByRole("button", { name: "Actions for e2e-proj-inst" }).click();
    }
    // The test-wide page.on("dialog") handler accepts the hx-confirm prompt.
    await projInstDelete.click();
    await expect(projInst).toHaveCount(0, { timeout: 1000 });
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

test("projects: delete an empty project straight from the list", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/projects");
  await page.getByRole("button", { name: "Create project" }).click();
  await page.locator('input[name="name"]').fill("e2e-rowdel");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page).toHaveURL(/\/projects\/e2e-rowdel$/);

  await page.goto("/projects");
  await expect(async () => {
    await page.getByRole("row", { name: /e2e-rowdel/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.getByRole("link", { name: "e2e-rowdel" })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("projects: usage and validated limits on the detail page", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/projects");
  await page.getByRole("button", { name: "Create project" }).click();
  await page.locator('input[name="name"]').fill("e2e-limits");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page).toHaveURL(/\/projects\/e2e-limits$/);

  // Scope to main: the collapsed Tasks footer echoes names in operation rows.
  const main = page.locator("main");
  const usage = main.locator("section", { has: page.getByRole("heading", { name: "Usage & limits" }) });
  await expect(usage.getByRole("cell", { name: "instances", exact: true })).toBeVisible();

  // Apply limits; the usage table picks them up.
  await usage.locator('input[name="instances"]').fill("5");
  await usage.locator('input[name="memory"]').fill("1GiB");
  await usage.getByRole("button", { name: "Apply limits" }).click();
  await expect(page).toHaveURL(/\/projects\/e2e-limits$/);
  await expect(usage.getByRole("row", { name: /instances/ }).getByRole("cell", { name: "5", exact: true })).toBeVisible();
  await expect(usage.getByRole("row", { name: /memory/ }).getByText("1.0 GiB")).toBeVisible();

  // Invalid values are rejected with a toast, nothing applied.
  await usage.locator('input[name="instances"]').fill("many");
  await usage.getByRole("button", { name: "Apply limits" }).click();
  await expect(page.locator("[data-tui-toast]")).toBeVisible();

  // The list's Resources column reports live instance usage.
  await page.goto("/projects");
  await expect(page.getByRole("row", { name: /e2e-limits/ }).getByText("0 instances")).toBeVisible();

  // Clean up for reruns against a reused server.
  await expect(async () => {
    await page.getByRole("row", { name: /e2e-limits/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.getByRole("link", { name: "e2e-limits" })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
