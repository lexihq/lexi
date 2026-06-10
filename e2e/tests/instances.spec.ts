import { test, expect } from "@playwright/test";

// Instance lifecycle through the list UI: create, start/stop/clone/delete,
// rename/move, restart/pause/resume, and the export/import round-trip.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("create from the image browser, then start/stop/clone/delete in the list", async ({ page }) => {
  const name = "e2e-create";

  // rowAction clicks a row button and waits for its HTMX POST to complete, so
  // the in-place row swap has settled before the next action or assertion.
  // Retry the click until the POST actually fires: a button in a freshly
  // htmx-swapped row can be clicked before its hx-post handler is bound (the
  // swap-then-click race), losing the click. We only re-click when no response
  // was observed, so a registered action is never fired twice.
  const rowAction = async (instance: string, button: string) => {
    // Only a lost click (no POST observed within the inner timeout) should retry;
    // a POST that returns an error still resolves waitForResponse, so the retry
    // never re-fires a registered action — the outer state assertions surface
    // real failures.
    await expect(async () => {
      await Promise.all([
        page.waitForResponse(
          (r) => r.request().method() === "POST" && r.url().includes(`/instances/${instance}/`),
          { timeout: 3000 },
        ),
        page.locator(`#instance-${instance}`).getByRole("button", { name: button, exact: true }).click(),
      ]);
    }).toPass({ timeout: 15000 });
  };

  await page.goto("/instances/new");

  // Typing fires the debounced HTMX search; pick the first filtered image.
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();

  await page.locator("#name").fill(name);
  await page.locator("input[name=start]").check();
  await page.getByRole("button", { name: "Create instance" }).click();

  // Full-page submit redirects to the list; the new row shows Running.
  await expect(page.locator(`#instance-${name}`)).toContainText(name);
  await expect(page.locator(`#instance-${name}`)).toContainText("Running");

  // Stop / Start swap the row in place over HTMX.
  await rowAction(name, "Stop");
  await expect(page.locator(`#instance-${name}`)).toContainText("Stopped");
  await rowAction(name, "Start");
  await expect(page.locator(`#instance-${name}`)).toContainText("Running");

  // Clone (full-page submit) adds a second row.
  const clone = `${name}-copy`;
  await page.locator(`#instance-${name} input[name=dst]`).fill(clone);
  await page.locator(`#instance-${name}`).getByRole("button", { name: "Clone" }).click();
  await expect(page.locator(`#instance-${clone}`)).toBeVisible();

  // Delete both rows; each removes itself from the table.
  for (const n of [clone, name]) {
    await rowAction(n, "Delete");
    await expect(page.locator(`#instance-${n}`)).toHaveCount(0);
  }
});

test("create with profile, pool, network, and initial config", async ({ page }) => {
  const name = "e2e-wizard";
  await page.goto("/instances/new");

  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill(name);

  // Optional selectors: gpu profile, explicit pool + network, a config key.
  await page.getByRole("checkbox", { name: "gpu" }).check();
  await page.locator("#create-pool").selectOption("default");
  await page.locator("#create-network").selectOption("incusbr0");
  await page.getByText("Advanced: initial config").click();
  await page.locator('form[action="/instances"] input[name="key"]').first().fill("user.e2e");
  await page.locator('form[action="/instances"] textarea[name="value"]').first().fill("wizard");
  await page.getByRole("button", { name: "Create instance" }).click();

  // The profile shows on the detail summary; the config key in the editor.
  await expect(page.locator(`#instance-${name}`)).toBeVisible();
  await page.goto(`/instances/${name}`);
  await expect(page.locator("#profiles").getByRole("checkbox", { name: "gpu" })).toBeChecked();
  await page.getByRole("link", { name: "Configuration" }).click();
  await expect(page.locator('input[name="key"][value="user.e2e"]')).toBeVisible();

  // Devices tab shows the injected root/eth0 local devices.
  await page.getByRole("link", { name: "Devices" }).click();
  await expect(page.locator("#devices").getByText("root", { exact: true })).toBeVisible();
  await expect(page.locator("#devices").getByText("eth0", { exact: true }).first()).toBeVisible();

  // Clean up from the list (shared server state).
  await page.goto("/");
  await expect(async () => {
    await page.locator(`#instance-${name}`).getByRole("button", { name: "Delete", exact: true }).click();
    await expect(page.locator(`#instance-${name}`)).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("create page arch and type filters narrow the image list", async ({ page }) => {
  await page.goto("/instances/new");
  const results = page.locator("#image-results");
  await expect(results).toContainText("debian/12");

  // Type filter: the fake catalog has a single virtual-machine image.
  await page.locator("select[name=type]").selectOption("virtual-machine");
  await expect(results).toContainText("ubuntu/24.04");
  await expect(results).toContainText("virtual-machine");
  await expect(results).not.toContainText("debian/12");

  // Reset type, then filter by architecture (alpine is aarch64-only).
  await page.locator("select[name=type]").selectOption("");
  await expect(results).toContainText("debian/12");
  await page.locator("select[name=arch]").selectOption("x86_64");
  await expect(results).toContainText("fedora/40");
  await expect(results).not.toContainText("alpine/edge");
});

test("rename and move an instance from the list row", async ({ page }) => {
  const name = "e2e-move";
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill(name);
  await page.getByRole("button", { name: "Create instance" }).click();

  // Rename (hx-boost=false → native POST, full navigation to the new detail page).
  const row = page.locator(`#instance-${name}`);
  await expect(row).toBeVisible();
  await row.locator('input[name="new_name"]').fill("e2e-moved");
  await row.getByRole("button", { name: "Rename" }).click();
  await expect(page).toHaveURL(/\/instances\/e2e-moved$/);

  // Move to a seeded pool from the list row (fake records it as a validated no-op).
  await page.goto("/");
  const moved = page.locator("#instance-e2e-moved");
  await expect(moved).toBeVisible();
  // The move input offers pool suggestions from the page-level datalist.
  await expect(page.locator("#pool-options option[value='default']")).toBeAttached();
  await moved.locator('input[name="pool"]').fill("default");
  await moved.getByRole("button", { name: "Move" }).click();
  await expect(page).toHaveURL(/\/instances\/e2e-moved$/);

  // Clean up.
  await page.goto("/");
  await expect(async () => {
    await page.locator("#instance-e2e-moved").getByRole("button", { name: "Delete", exact: true }).click();
    await expect(page.locator("#instance-e2e-moved")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("export downloads a tarball that re-imports as a new instance", async ({ page }) => {
  await page.goto("/instances/demo");

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("link", { name: "Export" }).click();
  const download = await downloadPromise;
  const file = await download.path();
  expect(file).toBeTruthy();

  await page.goto("/instances/import");
  const imported = "e2e-imported";
  await page.locator("#name").fill(imported);
  await page.locator("#backup").setInputFiles(file as string);
  await page.getByRole("button", { name: "Import instance" }).click();

  await expect(page.locator(`#instance-${imported}`)).toBeVisible();

  // Clean up the imported instance.
  await page.locator(`#instance-${imported}`).getByRole("button", { name: "Delete" }).click();
  await expect(page.locator(`#instance-${imported}`)).toHaveCount(0);
});

test("restart, pause, and resume an instance from the list row", async ({ page }) => {
  const name = "e2e-lifecycle";

  // Retry the click until the POST actually fires: a button in a freshly
  // htmx-swapped row can be clicked before its hx-post handler is bound (the
  // swap-then-click race), losing the click. We only re-click when no response
  // was observed, so a registered action is never fired twice.
  const rowAction = async (instance: string, button: string) => {
    // Only a lost click (no POST observed within the inner timeout) should retry;
    // a POST that returns an error still resolves waitForResponse, so the retry
    // never re-fires a registered action — the outer state assertions surface
    // real failures.
    await expect(async () => {
      await Promise.all([
        page.waitForResponse(
          (r) => r.request().method() === "POST" && r.url().includes(`/instances/${instance}/`),
          { timeout: 3000 },
        ),
        page.locator(`#instance-${instance}`).getByRole("button", { name: button, exact: true }).click(),
      ]);
    }).toPass({ timeout: 15000 });
  };

  // Create a running instance to exercise the lifecycle controls.
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill(name);
  await page.locator("input[name=start]").check();
  await page.getByRole("button", { name: "Create instance" }).click();

  const row = page.locator(`#instance-${name}`);
  await expect(row).toContainText("Running");

  // Restart leaves it Running.
  await rowAction(name, "Restart");
  await expect(row).toContainText("Running");

  // Pause freezes it; the Pause button gives way to Resume.
  await rowAction(name, "Pause");
  await expect(row).toContainText("Frozen");
  await expect(row.getByRole("button", { name: "Resume" })).toBeVisible();
  await expect(row.getByRole("button", { name: "Pause" })).toHaveCount(0);

  // Resume runs it again; Pause returns.
  await rowAction(name, "Resume");
  await expect(row).toContainText("Running");
  await expect(row.getByRole("button", { name: "Pause" })).toBeVisible();

  // Clean up.
  await rowAction(name, "Delete");
  await expect(row).toHaveCount(0);
});
