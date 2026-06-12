import { test, expect } from "@playwright/test";

// Remote switcher: the fake backend models two daemons ("local" with the
// seeded demo instance, "secondary" bare). Switching must scope the whole UI.

test("remotes: switch scopes the UI and clears the project selection", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  await page.goto("/");
  const remoteSelect = page.locator('aside select[name="remote"]');
  await expect(remoteSelect).toHaveValue("local");
  await expect(page.getByRole("link", { name: "demo" }).first()).toBeVisible();

  // Switch to the secondary remote: demo (a local-remote instance) vanishes.
  await remoteSelect.selectOption("secondary");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('aside select[name="remote"]')).toHaveValue("secondary");
  await expect(page.getByRole("link", { name: "demo" })).toHaveCount(0);

  // Resources created here land on this remote only.
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill("e2e-remote-inst");
  await page.getByRole("button", { name: "Create instance" }).click();
  await expect(page.locator("#instance-e2e-remote-inst")).toContainText("e2e-remote-inst");

  // Back on local: demo returns, the secondary instance is invisible.
  await page.locator('aside select[name="remote"]').selectOption("local");
  await expect(page.locator('aside select[name="remote"]')).toHaveValue("local");
  await expect(page.getByRole("link", { name: "demo" }).first()).toBeVisible();
  await expect(page.locator("#instance-e2e-remote-inst")).toHaveCount(0);

  // The project selection resets on a remote switch (names don't transfer
  // between daemons): switch with a project selected and verify the switcher
  // is back on default.
  await page.goto("/projects");
  await page.locator('input[name="name"]').fill("e2e-remote-proj");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(/\/projects\/e2e-remote-proj$/);
  await page.locator('select[name="project"]').selectOption("e2e-remote-proj");
  await expect(page.locator('select[name="project"]')).toHaveValue("e2e-remote-proj");

  await page.locator('aside select[name="remote"]').selectOption("secondary");
  await expect(page.locator('aside select[name="remote"]')).toHaveValue("secondary");
  await expect(page.locator('select[name="project"]')).toHaveValue("default");

  // Cleanup so reruns against a reused server start from the same state:
  // delete the secondary instance, return to local, delete the project.
  const row = page.locator("#instance-e2e-remote-inst");
  await expect(row).toBeVisible();
  const deleteItem = row.getByRole("menuitem", { name: "Delete", exact: true });
  await expect(async () => {
    if (!(await deleteItem.isVisible())) {
      await row.getByRole("button", { name: "Actions for e2e-remote-inst" }).click();
    }
    await deleteItem.click();
    await expect(row).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  await page.locator('aside select[name="remote"]').selectOption("local");
  // The switch submits the remote form (redirect to /); wait for local's
  // instance list before navigating on, or the goto cancels the switch.
  await expect(page.getByRole("link", { name: "demo" }).first()).toBeVisible();
  await page.goto("/projects");
  await expect(async () => {
    await page.getByRole("row", { name: /e2e-remote-proj/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.getByRole("link", { name: "e2e-remote-proj" })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("remotes: migrate a stopped instance to another remote and back", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  // Create a stopped instance on local.
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill("e2e-mig");
  await page.getByRole("button", { name: "Create instance" }).click();
  const row = page.locator("#instance-e2e-mig");
  await expect(row).toBeVisible();

  // Migrate… lives in the kebab menu for stopped instances; the dialog's
  // target select offers only the other remote.
  const migrate = async (name: string, newName: string) => {
    const r = page.locator(`#instance-${name}`);
    const item = r.getByRole("menuitem", { name: "Migrate…" });
    await expect(async () => {
      if (!(await item.isVisible())) {
        await r.getByRole("button", { name: `Actions for ${name}` }).click();
      }
      await item.click();
      await expect(page.locator(`#migrate-${name}`)).toBeVisible({ timeout: 1000 });
    }).toPass({ timeout: 10000 });
    const dialog = page.locator(`#migrate-${name}`);
    if (newName) {
      await dialog.locator('input[name="new_name"]').fill(newName);
    }
    await dialog.getByRole("button", { name: "Migrate", exact: true }).click();
  };

  await migrate("e2e-mig", "e2e-mig2");
  // The handler redirects to the list; the instance is gone from local.
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator("#instance-e2e-mig")).toHaveCount(0);

  // It lives on secondary under the new name; migrate it back and clean up.
  await page.locator('aside select[name="remote"]').selectOption("secondary");
  await expect(page.locator("#instance-e2e-mig2")).toBeVisible();
  await migrate("e2e-mig2", "");
  await expect(page.locator("#instance-e2e-mig2")).toHaveCount(0);

  await page.locator('aside select[name="remote"]').selectOption("local");
  const back = page.locator("#instance-e2e-mig2");
  await expect(back).toBeVisible();
  const deleteItem = back.getByRole("menuitem", { name: "Delete", exact: true });
  await expect(async () => {
    if (!(await deleteItem.isVisible())) {
      await back.getByRole("button", { name: "Actions for e2e-mig2" }).click();
    }
    await deleteItem.click();
    await expect(back).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
