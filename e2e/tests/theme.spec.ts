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

// The skip-to-content link: visually hidden until focused, first in tab order,
// and it targets the main content region for keyboard users.
test("skip-to-content link is the first focusable element and targets #main", async ({
  page,
}) => {
  await page.goto("/");

  await page.keyboard.press("Tab");
  const focused = page.locator(":focus");
  await expect(focused).toHaveText("Skip to content");
  await expect(focused).toHaveAttribute("href", /#main$/);
});

// The header tier badge: a button that opens a read-only popover listing the
// active driver tier's supported capabilities (only enabled ones are shown).
test("tier badge popover lists the active tier's capabilities", async ({
  page,
}) => {
  await page.goto("/");

  const trigger = page.getByRole("button", { name: "Tier capabilities" });
  // Scope to the capability popover by its unique "Tier:" heading; the list page
  // also has selectbox popovers and a "Snapshots" column header.
  const popover = page
    .locator("[data-tui-popover-content]")
    .filter({ hasText: "Tier:" });

  await expect(popover).toBeHidden();
  await trigger.click();
  await expect(popover).toBeVisible();
  // The e2e fakeserver enables these features, so they appear in the list.
  await expect(popover.getByText("Snapshots")).toBeVisible();
  await expect(popover.getByText("Metrics")).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(popover).toBeHidden();
});
