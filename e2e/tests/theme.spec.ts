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

// The header palette picker: a separate axis from light/dark. Selecting a
// palette sets data-theme on <html>, persists it, reapplies it on the next
// load, and marks the active entry with a check (aria-checked).
test("palette picker switches, persists, and marks the active theme", async ({
  page,
}) => {
  await page.goto("/");

  // Start from the built-in default palette regardless of any stored choice.
  await page.evaluate(() => {
    localStorage.removeItem("color-theme");
    document.documentElement.removeAttribute("data-theme");
  });
  await expect(page.locator("html")).not.toHaveAttribute("data-theme", /.+/);

  // Selecting Ocean sets the attribute and records the choice.
  await page.getByRole("button", { name: "Theme palette" }).click();
  await page.getByRole("menuitemradio", { name: "Ocean" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "ocean");
  expect(await page.evaluate(() => localStorage.getItem("color-theme"))).toBe(
    "ocean",
  );

  // A full navigation keeps the palette (applied by the head script).
  await page.goto("/storage");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "ocean");

  // Reopening the picker shows Ocean as the active (checked) entry.
  await page.getByRole("button", { name: "Theme palette" }).click();
  await expect(
    page.getByRole("menuitemradio", { name: "Ocean" }),
  ).toHaveAttribute("aria-checked", "true");

  // Switching back to Default removes the attribute and clears the choice.
  await page.getByRole("menuitemradio", { name: "Default" }).click();
  await expect(page.locator("html")).not.toHaveAttribute("data-theme", /.+/);
  expect(await page.evaluate(() => localStorage.getItem("color-theme"))).toBe(
    "",
  );
});
