import { test, expect } from "@playwright/test";

// The instance detail page: header lifecycle controls, tabs, and the
// logs/metrics panels.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("detail header lifecycle controls update status in place", async ({ page }) => {
  await page.goto("/instances/demo");
  const header = page.locator("#instance-header");
  await expect(header.getByText("Stopped")).toBeVisible(); // seeded state

  // Start from the header: status flips without a navigation.
  await header.getByRole("button", { name: "Start", exact: true }).click();
  await expect(header.getByText("Running")).toBeVisible();
  await expect(page).toHaveURL(/\/instances\/demo$/);

  // Stop again so later tests see the seeded state.
  await header.getByRole("button", { name: "Stop", exact: true }).click();
  await expect(header.getByText("Stopped")).toBeVisible();
});

test("logs panel refresh button re-fetches the console log", async ({ page }) => {
  await page.goto("/instances/demo");
  // The Logs panel now lives behind the Logs tab; opening it mounts #logs.
  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("#logs")).toContainText("Console log");

  // The Refresh button is freshly swapped in by the panel's load trigger, so
  // the first click can be lost to the swap-then-click settle race (suite-wide
  // pattern). Retry until the GET actually fires.
  await expect(async () => {
    const [resp] = await Promise.all([
      page.waitForResponse(
        (r) => r.request().method() === "GET" && r.url().includes("/instances/demo/logs"),
        { timeout: 2000 },
      ),
      page.locator("#logs").getByRole("button", { name: "Refresh" }).click(),
    ]);
    expect(resp.ok()).toBeTruthy();
  }).toPass({ timeout: 10000 });
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

test("metrics tab renders history charts that update", async ({ page }) => {
  let seriesRequests = 0;
  page.on("response", (r) => {
    if (r.url().includes("/instances/demo/metrics/series")) seriesRequests += 1;
  });

  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Metrics" }).click();

  const charts = page.locator("#metrics-charts");
  await expect(charts).toContainText("Resource history");

  // metrics-charts.js draws a uPlot canvas into each chart slot.
  await expect(charts.locator("#mc-cpu canvas").first()).toBeVisible();
  await expect(charts.locator("#mc-mem canvas").first()).toBeVisible();
  await expect(charts.locator("#mc-net canvas").first()).toBeVisible();

  // The charts poll the JSON series endpoint to accumulate history.
  await expect.poll(() => seriesRequests, { timeout: 8_000 }).toBeGreaterThanOrEqual(2);

  // Leaving the tab tears the charts down (the polling loop self-destructs).
  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("#metrics-charts")).toHaveCount(0);
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
  // Applying limits emits an out-of-band success toast without disturbing the form.
  await expect(page.locator('[data-tui-toast][data-variant="success"]')).toBeVisible();

  // The Metrics and Logs panels each live behind their own tab.
  await page.getByRole("link", { name: "Metrics" }).click();
  await expect(page.locator("#metrics")).toContainText("Memory");

  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("#logs")).toContainText("Console log");
});
