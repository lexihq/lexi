import { test, expect } from "@playwright/test";

// The header dark-mode toggle: flips the .dark class on <html>, persists the
// choice, and reapplies it on the next page load (before paint, no flash).

test("theme toggle switches and persists dark mode across navigation", async ({
  page,
}) => {
  await page.goto("/");

  // Start from a known light state regardless of the runner's OS preference.
  await page.evaluate(() => {
    localStorage.removeItem("theme");
    document.documentElement.classList.remove("dark");
  });
  await expect(page.locator("html")).not.toHaveClass(/dark/);

  // Toggling adds .dark and records the choice.
  await page.getByRole("button", { name: "Toggle dark mode" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  expect(await page.evaluate(() => localStorage.getItem("theme"))).toBe("dark");

  // A full navigation keeps the dark theme (applied by the head script).
  await page.goto("/storage");
  await expect(page.locator("html")).toHaveClass(/dark/);

  // Toggling back returns to light and persists that.
  await page.getByRole("button", { name: "Toggle dark mode" }).click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  expect(await page.evaluate(() => localStorage.getItem("theme"))).toBe("light");
});
