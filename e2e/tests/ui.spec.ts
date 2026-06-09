import { test, expect } from "@playwright/test";

// End-to-end coverage of the UI actions that unit/Go tests exercise only at the
// handler layer: the create flow, list-row actions, snapshots, limits, metrics,
// logs, and the export/import round-trip — all driven through a real browser
// against the fake backend. The fakeserver seeds an instance named "demo".

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

test("logs panel refresh button re-fetches the console log", async ({ page }) => {
  await page.goto("/instances/demo");
  // The Logs panel now lives behind the Logs tab; opening it mounts #logs.
  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("#logs")).toContainText("Console log");

  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "GET" && r.url().includes("/instances/demo/logs"),
    ),
    page.locator("#logs").getByRole("button", { name: "Refresh" }).click(),
  ]);
  await expect(page.locator("#logs")).toContainText("demo booted");
});

test("metrics panel polls for updates", async ({ page }) => {
  let metricsRequests = 0;
  page.on("response", (r) => {
    if (r.url().includes("/instances/demo/metrics")) metricsRequests += 1;
  });

  await page.goto("/instances/demo");
  // The metrics poll only runs while its tab is mounted.
  await page.getByRole("link", { name: "Metrics" }).click();
  await expect(page.locator("#metrics")).toContainText("Live metrics");

  // The panel reloads itself every 3s; the initial load plus one poll is >= 2.
  await expect.poll(() => metricsRequests, { timeout: 8_000 }).toBeGreaterThanOrEqual(2);
});

test("detail tabs expose summary limits, metrics, and logs", async ({ page }) => {
  await page.goto("/instances/demo");

  // Summary is the default tab: apply resource limits in place.
  await page.locator("#cpu").fill("2");
  await page.locator("#memory").fill("512MiB");
  await page.getByRole("button", { name: "Apply limits" }).click();

  // The form re-renders in place reflecting the applied values.
  await expect(page.locator("#cpu")).toHaveValue("2");
  await expect(page.locator("#memory")).toHaveValue("512MiB");

  // The Metrics and Logs panels each live behind their own tab.
  await page.getByRole("link", { name: "Metrics" }).click();
  await expect(page.locator("#metrics")).toContainText("Memory");

  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("#logs")).toContainText("Console log");
});

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

test("edit instance config in the Configuration tab", async ({ page }) => {
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Configuration" }).click();
  const config = page.locator("#config");
  await expect(config.getByRole("button", { name: "Apply config" })).toBeVisible();

  // Add a key via the trailing blank row.
  await config.locator('input[name="key"]').last().fill("security.nesting");
  await config.locator('input[name="value"]').last().fill("true");
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/config"),
    ),
    config.getByRole("button", { name: "Apply config" }).click(),
  ]);
  await expect(page.locator('#config input[value="security.nesting"]')).toBeVisible();

  // Remove it: clear the key and apply.
  await page.locator('#config input[value="security.nesting"]').fill("");
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/config"),
    ),
    page.locator("#config").getByRole("button", { name: "Apply config" }).click(),
  ]);
  await expect(page.locator('#config input[value="security.nesting"]')).toHaveCount(0);
});

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

test("create and delete a custom volume in the Storage section", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Storage" }).click();
  await expect(page).toHaveURL(/\/storage$/);
  await page.getByRole("link", { name: "default" }).click();
  await expect(page).toHaveURL(/\/storage\/default$/);

  const volumes = page.locator("#volumes");
  await expect(volumes).toBeVisible();
  await volumes.locator('input[name="name"]').fill("e2e-vol");
  await page.getByRole("button", { name: "Create volume" }).click();

  await expect(page).toHaveURL(/\/storage\/default$/);
  await expect(page.locator("#volumes").getByText("e2e-vol")).toBeVisible();

  // Same htmx swap-then-click race as the snapshot/device Delete: retry until
  // the delete takes effect (a single lost click would otherwise fail).
  await expect(async () => {
    await page.locator("#volumes").getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volumes").getByText("e2e-vol")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("snapshot a custom volume: create, restore, and delete", async ({ page }) => {
  await page.goto("/storage/default");
  // Distinct volume name so this doesn't collide with the volume-CRUD test on
  // the shared fake server.
  await page.locator("#volumes").locator('input[name="name"]').fill("e2e-snapvol");
  await page.getByRole("button", { name: "Create volume" }).click();
  await page.locator("#volumes").getByRole("link", { name: "e2e-snapvol" }).click();
  await expect(page).toHaveURL(/\/storage\/default\/volumes\/e2e-snapvol$/);

  const snaps = page.locator("#volume-snapshots");
  await expect(snaps).toBeVisible();
  await snaps.locator('input[name="snapshot"]').fill("snap0");
  await page.getByRole("button", { name: "Create snapshot" }).click();
  await expect(snaps).toContainText("snap0");

  await snaps.getByRole("button", { name: "Restore" }).click();
  await expect(snaps).toContainText("snap0");

  // htmx swap-then-click race: retry the delete until it takes effect.
  await expect(async () => {
    await page.locator("#volume-snapshots").getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volume-snapshots").getByText("snap0")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Clean up the volume from the pool page (shared server state).
  await page.goto("/storage/default");
  await expect(async () => {
    await page.locator("#volumes").getByRole("row", { name: /e2e-snapvol/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#volumes").getByText("e2e-snapvol")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
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

test("manage local images: copy, publish, alias add/remove, delete", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Images" }).click();
  await expect(page).toHaveURL(/\/images$/);
  const table = page.locator("#images-table");
  await expect(table.getByText("debian/12")).toBeVisible();

  // Copy a catalog alias into the local store. The copy/publish forms live
  // outside the swapped table, so they have no swap-then-click race.
  await page.locator('form[hx-post="/images/copy"] input[name="alias"]').fill("alpine/edge");
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await expect(table.getByText("alpine/edge")).toBeVisible();

  // Publish the seeded (stopped) instance as an image with an alias.
  await page.locator('form[hx-post="/images/publish"] select[name="instance"]').selectOption("demo");
  await page.locator('form[hx-post="/images/publish"] input[name="alias"]').fill("e2e-pub");
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(table.getByText("e2e-pub")).toBeVisible();

  // Add an alias on the published row. Row controls live in freshly-swapped
  // content, so retry lost clicks (the usual swap-then-click race); a retry
  // after a successful add just 409s without changing the table.
  await expect(async () => {
    const row = table.getByRole("row", { name: /e2e-pub/ });
    await row.locator('input[name="alias"]').fill("e2e-extra");
    await row.locator('button[title="Add alias"]').click();
    await expect(table.getByText("e2e-extra")).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Remove that alias via its chip button.
  await expect(async () => {
    await page.locator('button[title="Remove alias e2e-extra"]').click();
    await expect(table.getByText("e2e-extra")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  // Delete the published image.
  await expect(async () => {
    await table.getByRole("row", { name: /e2e-pub/ }).getByRole("button", { name: "Delete" }).click();
    await expect(table.getByText("e2e-pub")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("tasks panel lists operations and picks up new ones", async ({ page }) => {
  const name = "e2e-task";

  // Creating an instance records an operation in the fake's task log.
  await page.goto("/instances/new");
  await page.locator("#image-search").pressSequentially("debian");
  const firstImage = page.locator("#image-results input[type=radio][name=image]").first();
  await expect(firstImage).toBeVisible();
  await firstImage.check();
  await page.locator("#name").fill(name);
  await page.getByRole("button", { name: "Create instance" }).click();
  const row = page.locator(`#instance-${name}`);
  await expect(row).toBeVisible();

  // Expand the bottom Tasks panel; its content hx-loads on page load.
  const footer = page.locator("footer");
  await footer.locator('label[for="ops-toggle"]').click();
  await expect(footer.getByText(`Creating instance "${name}"`)).toBeVisible();

  // Delete the instance; the 5s poll picks the new operation up.
  await expect(async () => {
    await row.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(row).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
  await expect(footer.getByText(`Deleting instance "${name}"`)).toBeVisible({ timeout: 10000 });
});
