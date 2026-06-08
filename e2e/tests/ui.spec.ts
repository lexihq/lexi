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
      page.locator(`#instance-${instance}`).getByRole("button", { name: button }).click(),
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
  await expect(page.locator("#metrics")).toContainText("Live metrics");

  // The panel reloads itself every 3s; the initial load plus one poll is >= 2.
  await expect.poll(() => metricsRequests, { timeout: 8_000 }).toBeGreaterThanOrEqual(2);
});

test("detail page renders metrics and logs and applies resource limits", async ({ page }) => {
  await page.goto("/instances/demo");

  await expect(page.locator("#metrics")).toContainText("Live metrics");
  await expect(page.locator("#metrics")).toContainText("Memory");
  await expect(page.locator("#logs")).toContainText("Console log");

  await page.locator("#cpu").fill("2");
  await page.locator("#memory").fill("512MiB");
  await page.getByRole("button", { name: "Apply limits" }).click();

  // The form re-renders in place reflecting the applied values.
  await expect(page.locator("#cpu")).toHaveValue("2");
  await expect(page.locator("#memory")).toHaveValue("512MiB");
});

test("snapshot create, restore, and delete on the detail page", async ({ page }) => {
  await page.goto("/instances/demo");
  const snap = "e2e-snap";
  const snapshots = page.locator("#snapshots");

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
