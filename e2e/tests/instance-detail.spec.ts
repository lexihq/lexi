import { test, expect } from "@playwright/test";

// The instance detail page: header lifecycle controls, tabs, and the
// logs/metrics panels.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("detail header lifecycle controls update status in place", async ({ page }) => {
  await page.goto("/instances/demo");
  const header = page.locator("#instance-header");

  // The header re-swaps itself on every action, so each freshly-swapped button
  // can lose its first click to the settle race (suite-wide pattern). Retry the
  // click until the lifecycle POST actually fires, then assert the new status.
  const act = async (button: string) => {
    await expect(async () => {
      const [resp] = await Promise.all([
        page.waitForResponse(
          (r) => r.request().method() === "POST" && r.url().includes("/instances/demo/"),
          { timeout: 2000 },
        ),
        header.getByRole("button", { name: button, exact: true }).click(),
      ]);
      expect(resp.ok()).toBeTruthy();
    }).toPass({ timeout: 10000 });
  };

  await expect(header.getByText("Stopped")).toBeVisible(); // seeded state

  // Start from the header: status flips without a navigation.
  await act("Start");
  await expect(header.getByText("Running")).toBeVisible();
  await expect(page).toHaveURL(/\/instances\/demo$/);

  // Stop again so later tests see the seeded state.
  await act("Stop");
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

test("metrics chart axes are theme-aware (legible) in dark mode", async ({ page }) => {
  // theme.js applies the stored preference to <html> before first paint.
  await page.addInitScript(() => localStorage.setItem("theme", "dark"));
  await page.goto("/instances/demo");
  await expect(page.locator("html")).toHaveClass(/dark/);

  await page.getByRole("link", { name: "Metrics" }).click();
  const cpuCanvas = page.locator("#metrics-charts #mc-cpu canvas").first();
  await expect(cpuCanvas).toBeVisible();

  // The pre-fix bug drew axis tick labels in uPlot's default pure black (~0
  // luminance), invisible on the dark card; the fix paints them in the
  // muted-foreground token (~rgb 161). Sample the left axis gutter (labels only,
  // left of the plot/data line) and assert a label pixel is light. Polled because
  // the labels draw once the first series tick sets the scale.
  const maxGutterLuminance = () =>
    cpuCanvas.evaluate((cv: HTMLCanvasElement) => {
      const ctx = cv.getContext("2d");
      if (!ctx) return 0;
      const { data } = ctx.getImageData(0, 0, 36, cv.height);
      let max = 0;
      for (let i = 0; i < data.length; i += 4) {
        if (data[i + 3] < 120) continue; // ignore translucent gridlines/edges
        const lum = 0.2126 * data[i] + 0.7152 * data[i + 1] + 0.0722 * data[i + 2];
        if (lum > max) max = lum;
      }
      return max;
    });
  await expect.poll(maxGutterLuminance, { timeout: 8_000 }).toBeGreaterThan(120);
});

test("detail tabs expose configuration limits, metrics, and logs", async ({ page }) => {
  await page.goto("/instances/demo");

  // The limits editor lives on the Configuration tab (Summary is read-only).
  await page.getByRole("link", { name: "Configuration" }).click();
  await page.locator("#cpu").fill("2");
  await page.locator("#memory").fill("512MiB");
  await page.getByRole("button", { name: "Apply limits" }).click();

  // The form re-renders in place reflecting the applied values.
  await expect(page.locator("#cpu")).toHaveValue("2");
  await expect(page.locator("#memory")).toHaveValue("512MiB");
  // Applying limits emits an out-of-band success toast without disturbing the form.
  await expect(page.locator('[data-tui-toast][data-variant="success"]')).toBeVisible();

  // The Summary tab reflects the applied limits read-only.
  await page.getByRole("link", { name: "Summary" }).click();
  await expect(page.getByRole("main")).toContainText("2 / 512MiB");

  // The Metrics and Logs panels each live behind their own tab.
  await page.getByRole("link", { name: "Metrics" }).click();
  await expect(page.locator("#metrics")).toContainText("Memory");

  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("#logs")).toContainText("Console log");
});
