import { test, expect } from "@playwright/test";

// End-to-end coverage of the UI actions that unit/Go tests exercise only at the
// handler layer: the create flow, list-row actions, snapshots, limits, metrics,
// logs, and the export/import round-trip — all driven through a real browser
// against the fake backend. The fakeserver seeds an instance named "demo".

test("create from the image browser, then start/stop/clone/delete in the list", async ({ page }) => {
  const name = "e2e-create";

  // rowAction clicks a row button and waits for its HTMX POST to complete, so
  // the in-place row swap has settled before the next action or assertion.
  const rowAction = async (instance: string, button: string) => {
    await Promise.all([
      page.waitForResponse(
        (r) => r.request().method() === "POST" && r.url().includes(`/instances/${instance}/`),
      ),
      page.locator(`#instance-${instance}`).getByRole("button", { name: button, exact: true }).click(),
    ]);
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

  await snapshots.getByRole("button", { name: "Delete" }).click();
  await expect(snapshots).not.toContainText(snap);
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

  const rowAction = async (instance: string, button: string) => {
    await Promise.all([
      page.waitForResponse(
        (r) => r.request().method() === "POST" && r.url().includes(`/instances/${instance}/`),
      ),
      page.locator(`#instance-${instance}`).getByRole("button", { name: button, exact: true }).click(),
    ]);
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

  // Remove it via the Remove button on the local device row.
  await devices.getByRole("button", { name: "Remove" }).click();
  await expect(devices.getByText("web", { exact: true })).toHaveCount(0);
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

test("backend errors surface as a toast", async ({ page }) => {
  await page.goto("/networks/new");
  // incusbr0 is seeded → creating it again conflicts (409).
  await page.locator('input[name="name"]').fill("incusbr0");
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page.locator("[data-tui-toast]")).toBeVisible();
  // The form is not replaced by the error response.
  await expect(page.locator('input[name="name"]')).toBeVisible();
});
