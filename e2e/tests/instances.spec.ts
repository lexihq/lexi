import { test, expect, type Page } from "@playwright/test";

// Instance lifecycle through the list UI: create, start/stop/clone/delete,
// rename/move, restart/pause/resume, and the export/import round-trip.
// All tests run against the shared fake-backed server (instance "demo" seeded).
//
// Row actions live in two places: a status-aware primary button (Start/Stop/
// Resume) and a kebab menu ("Actions for <name>") holding everything else.
// Clone/Rename/Move open per-row dialogs that submit a full-page POST.

// primaryAction clicks the row's status-aware button and waits for its HTMX
// POST to complete, so the in-place row swap has settled before the next
// action or assertion. Retry the click until the POST actually fires: a button
// in a freshly htmx-swapped row can be clicked before its hx-post handler is
// bound (the swap-then-click race), losing the click. We only re-click when no
// response was observed, so a registered action is never fired twice.
const primaryAction = async (page: Page, instance: string, button: string) => {
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

// menuAction opens the row's kebab menu and clicks a menu item, waiting for
// its HTMX POST. The menu content renders inside the row, so everything is
// scoped to it. Destructive items use hx-confirm (a native confirm dialog),
// accepted via opts.confirm. The toPass retry covers the same swap-then-click
// race as primaryAction; re-clicking the trigger on a retry merely toggles the
// menu, which the visibility guard below absorbs.
const menuAction = async (
  page: Page,
  instance: string,
  item: string,
  opts: { confirm?: boolean } = {},
) => {
  const row = page.locator(`#instance-${instance}`);
  const menuItem = row.getByRole("menuitem", { name: item, exact: true });
  // One persistent handler for the whole retry loop: stacking a page.once per
  // attempt would invoke several accepts on the same dialog, which throws.
  const acceptDialog = (d: import("@playwright/test").Dialog) => d.accept().catch(() => {});
  if (opts.confirm) {
    page.on("dialog", acceptDialog);
  }
  try {
    await expect(async () => {
      if (!(await menuItem.isVisible())) {
        await row.getByRole("button", { name: `Actions for ${instance}` }).click();
      }
      await expect(menuItem).toBeVisible({ timeout: 2000 });
      await Promise.all([
        page.waitForResponse(
          (r) => r.request().method() === "POST" && r.url().includes(`/instances/${instance}/`),
          { timeout: 3000 },
        ),
        menuItem.click(),
      ]);
    }).toPass({ timeout: 15000 });
  } finally {
    if (opts.confirm) {
      page.off("dialog", acceptDialog);
    }
  }
};

// dialogAction opens a row dialog via the kebab menu and submits its
// single-input form. The submit is a full-page POST (hx-boost off), so callers
// assert on the resulting navigation instead of an HTMX response.
const dialogAction = async (
  page: Page,
  instance: string,
  item: string,
  dialogId: string,
  field: string,
  value: string,
  submit: string,
) => {
  const row = page.locator(`#instance-${instance}`);
  await row.getByRole("button", { name: `Actions for ${instance}` }).click();
  await row.getByRole("menuitem", { name: item, exact: true }).click();
  const dlg = page.locator(`#${dialogId} dialog`);
  await expect(dlg).toBeVisible();
  await dlg.locator(`input[name=${field}]`).fill(value);
  await dlg.getByRole("button", { name: submit, exact: true }).click();
};

test("create from the image browser, then start/stop/clone/delete in the list", async ({ page }) => {
  const name = "e2e-create";

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

  // Stop / Start swap the row in place over HTMX, flipping the primary button.
  await primaryAction(page, name, "Stop");
  await expect(page.locator(`#instance-${name}`)).toContainText("Stopped");
  await expect(page.locator(`#instance-${name}`).getByRole("button", { name: "Start" })).toBeVisible();
  await primaryAction(page, name, "Start");
  await expect(page.locator(`#instance-${name}`)).toContainText("Running");
  await expect(page.locator(`#instance-${name}`).getByRole("button", { name: "Stop" })).toBeVisible();

  // Clone via the kebab dialog (full-page submit) adds a second row.
  const clone = `${name}-copy`;
  await dialogAction(page, name, "Clone…", `clone-${name}`, "dst", clone, "Clone");
  await expect(page.locator(`#instance-${clone}`)).toBeVisible();

  // Delete both rows from the kebab menu; each removes itself from the table.
  for (const n of [clone, name]) {
    await menuAction(page, n, "Delete", { confirm: true });
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
  await menuAction(page, name, "Delete", { confirm: true });
  await expect(page.locator(`#instance-${name}`)).toHaveCount(0);
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
  await expect(page.locator(`#instance-${name}`)).toBeVisible();

  // Rename via the kebab dialog (full-page POST, navigates to the new detail page).
  await dialogAction(page, name, "Rename…", `rename-${name}`, "new_name", "e2e-moved", "Rename");
  await expect(page).toHaveURL(/\/instances\/e2e-moved$/);

  // Move to a seeded pool from the list row (fake records it as a validated no-op).
  await page.goto("/");
  await expect(page.locator("#instance-e2e-moved")).toBeVisible();
  // The move dialog's input offers pool suggestions from the page-level datalist.
  await expect(page.locator("#pool-options option[value='default']")).toBeAttached();
  await dialogAction(page, "e2e-moved", "Move…", "move-e2e-moved", "pool", "default", "Move");
  await expect(page).toHaveURL(/\/instances\/e2e-moved$/);

  // Clean up.
  await page.goto("/");
  await menuAction(page, "e2e-moved", "Delete", { confirm: true });
  await expect(page.locator("#instance-e2e-moved")).toHaveCount(0);
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
  await menuAction(page, imported, "Delete", { confirm: true });
  await expect(page.locator(`#instance-${imported}`)).toHaveCount(0);
});

test("restart, pause, and resume an instance from the list row", async ({ page }) => {
  const name = "e2e-lifecycle";

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

  // Restart (kebab menu) leaves it Running.
  await menuAction(page, name, "Restart");
  await expect(row).toContainText("Running");

  // Pause freezes it; the primary button becomes Resume.
  await menuAction(page, name, "Pause");
  await expect(row).toContainText("Frozen");
  await expect(row.getByRole("button", { name: "Resume" })).toBeVisible();

  // Resume runs it again; the primary button returns to Stop.
  await primaryAction(page, name, "Resume");
  await expect(row).toContainText("Running");
  await expect(row.getByRole("button", { name: "Stop" })).toBeVisible();

  // Clean up.
  await menuAction(page, name, "Delete", { confirm: true });
  await expect(row).toHaveCount(0);
});
