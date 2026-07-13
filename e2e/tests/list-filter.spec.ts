import { test, expect, type Page } from "./fixtures";

// The instances toolbar (list-filter.js) is pure client-side DOM state: it must
// narrow rows without matching each row's hidden menu/dialog text, survive the
// idle refresh replacing #instances-table wholesale, and never let the bulk
// select-all sweep up rows the filter hid — bulk actions post every checked
// box, visible or not.

const create = async (page: Page, name: string) => {
  const res = await page.request.post("/instances", {
    form: { name, image: "fake-debian-12-aarch64" },
  });
  expect(res.ok(), `create ${name}`).toBeTruthy();
};

const remove = async (page: Page, name: string) => {
  const res = await page.request.post(`/instances/${name}/delete`, {
    form: { confirm: name },
  });
  expect(res.ok(), `delete ${name}`).toBeTruthy();
};

test("text filter narrows rows, ignores row chrome, and survives the table swap", async ({ page }) => {
  await create(page, "filter-a");
  await create(page, "filter-b");
  try {
    await page.goto("/");
    const input = page.locator("[data-list-filter]");
    await input.fill("filter-a");
    await expect(page.locator("#instance-filter-a")).toBeVisible();
    await expect(page.locator("#instance-filter-b")).toBeHidden();
    await expect(page.locator("#instance-demo")).toBeHidden();

    // Words that only occur in each row's kebab menu and dialogs ("Console",
    // "Delete", …) must not match: they are chrome, not data.
    await input.fill("console");
    await expect(page.locator("#instance-filter-a")).toBeHidden();
    await expect(page.locator("#instance-demo")).toBeHidden();

    // The idle refresh replaces the whole table fragment; the active filter
    // re-applies to the fresh rows.
    await input.fill("filter-a");
    await expect(page.locator("#instance-filter-b")).toBeHidden();
    await page.evaluate(() =>
      (window as unknown as { htmx: { ajax: (v: string, p: string, o: object) => void } }).htmx.ajax(
        "GET",
        "/partials/instances",
        { target: "#instances-table", swap: "outerHTML" },
      ),
    );
    await expect(page.locator("#instance-filter-a")).toBeVisible();
    await expect(page.locator("#instance-filter-b")).toBeHidden();
    await expect(input).toHaveValue("filter-a");
  } finally {
    await remove(page, "filter-a");
    await remove(page, "filter-b");
  }
});

test("select-all skips rows hidden by the filter", async ({ page }) => {
  await create(page, "filter-a");
  await create(page, "filter-b");
  try {
    await page.goto("/");
    const filter = page.locator("[data-list-filter]");
    const all = page.locator("[data-bulk-all]");
    await filter.fill("filter-a");
    await expect(page.locator("#instance-filter-b")).toBeHidden();

    // .click(), not .check(): with the hidden rows left unselected the header
    // box lands indeterminate, which check() reports as a failed click.
    await all.click();
    await expect(page.locator("#instance-filter-a [data-bulk-cb]")).toBeChecked();
    // The hidden rows must not be silently selected: a bulk delete would hit
    // instances the operator never saw.
    await expect(page.locator("#instance-filter-b [data-bulk-cb]")).not.toBeChecked();
    await expect(page.locator("#instance-demo [data-bulk-cb]")).not.toBeChecked();

    // The other half of the trap: narrowing the filter deselects the rows it
    // hides (list-filter dispatches change), so a selection can never linger
    // out of sight.
    await filter.fill("");
    await expect(page.locator("#instance-filter-b")).toBeVisible();
    await all.click(); // indeterminate -> checked: selects every visible row
    await expect(page.locator("#instance-filter-b [data-bulk-cb]")).toBeChecked();
    await filter.fill("filter-a");
    await expect(page.locator("#instance-filter-b")).toBeHidden();
    await expect(page.locator("#instance-filter-b [data-bulk-cb]")).not.toBeChecked();
    await expect(page.locator("#instance-demo [data-bulk-cb]")).not.toBeChecked();
    await expect(page.locator("#instance-filter-a [data-bulk-cb]")).toBeChecked();
  } finally {
    await remove(page, "filter-a");
    await remove(page, "filter-b");
  }
});

test("click-to-sort orders by name and toggles direction", async ({ page }) => {
  await create(page, "aaa-first");
  try {
    await page.goto("/");
    const nameHead = page.locator("#instances-table th[data-sort]", { hasText: "Name" });
    await nameHead.click();
    await expect(nameHead).toHaveAttribute("aria-sort", "ascending");
    await expect(page.locator("#instances-table tbody tr:visible").first()).toContainText("aaa-first");

    await nameHead.click();
    await expect(nameHead).toHaveAttribute("aria-sort", "descending");
    await expect(page.locator("#instances-table tbody tr:visible").first()).not.toContainText("aaa-first");
  } finally {
    await remove(page, "aaa-first");
  }
});
