import { defineConfig, devices } from "@playwright/test";
import path from "node:path";

// The fake-backed server is started for the whole suite and torn down after.
// It runs from the repo root so `go run` resolves the module, and seeds a
// single instance named "demo" that the console test drives.
const PORT = 8099;
const BASE_URL = `http://127.0.0.1:${PORT}`;

export default defineConfig({
  testDir: "./tests",
  // Serial: the fake backend is one shared in-memory process, so tests that
  // create/delete instances must not race each other.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `go run ./internal/e2e/fakeserver --addr 127.0.0.1:${PORT}`,
    cwd: path.resolve(__dirname, ".."),
    url: `${BASE_URL}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
