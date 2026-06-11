import { test, expect } from "@playwright/test";

// Remote switcher: the fake backend models two daemons ("local" with the
// seeded demo instance, "secondary" bare). Switching must scope the whole UI.

test("remotes: switch scopes the UI and clears the project selection", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  await page.goto("/");
  const remoteSelect = page.locator('select[name="remote"]');
  await expect(remoteSelect).toHaveValue("local");
  await expect(page.getByRole("link", { name: "demo" }).first()).toBeVisible();

  // Switch to the secondary remote: demo (a local-remote instance) vanishes.
  await remoteSelect.selectOption("secondary");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('select[name="remote"]')).toHaveValue("secondary");
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
  await page.locator('select[name="remote"]').selectOption("local");
  await expect(page.locator('select[name="remote"]')).toHaveValue("local");
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

  await page.locator('select[name="remote"]').selectOption("secondary");
  await expect(page.locator('select[name="remote"]')).toHaveValue("secondary");
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

  await page.locator('select[name="remote"]').selectOption("local");
  await page.goto("/projects");
  await expect(async () => {
    await page.getByRole("row", { name: /e2e-remote-proj/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.getByRole("link", { name: "e2e-remote-proj" })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
