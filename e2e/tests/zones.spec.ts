import { test, expect } from "./fixtures";

// DNS zones live under the Networks section. The fake backend starts with no
// zones; the test creates, edits, records, and deletes its own.

test("DNS zones: create, add a record, edit config, and delete", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  // Reachable from the Networks page, gated by the capability.
  await page.goto("/networks");
  await page.getByRole("link", { name: "DNS zones" }).click();
  await expect(page).toHaveURL(/\/network-zones$/);

  // Scope to main: the collapsed Tasks footer echoes names in operation rows.
  const main = page.locator("main");

  await page.locator('input[name="name"]').fill("e2e.example.org");
  await page.locator('input[name="description"]').fill("e2e zone");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(/\/network-zones\/e2e.example.org$/);

  // Add an A record; it lands in the records table with its TTL.
  await page.locator('input[name="record"]').fill("www");
  await page.locator('select[name="type"]').selectOption("A");
  await page.locator('input[name="value"]').fill("10.0.3.99");
  await page.locator('input[name="ttl"]').fill("300");
  await page.getByRole("button", { name: "Add record" }).click();
  await expect(page).toHaveURL(/\/network-zones\/e2e.example.org$/);
  await expect(main.getByRole("cell", { name: "www" })).toBeVisible();
  await expect(main.getByText("A 10.0.3.99")).toBeVisible();
  await expect(main.getByText("TTL 300s")).toBeVisible();

  // Versioned config editor: set a nameserver key.
  const config = main.locator('form[action="/network-zones/e2e.example.org/config"]');
  await config.locator('input[name="key"]').last().fill("dns.nameservers");
  await config.locator('textarea[name="value"]').last().fill("ns1.e2e.example.org");
  await config.getByRole("button", { name: "Apply config" }).click();
  await expect(page).toHaveURL(/\/network-zones\/e2e.example.org$/);
  await expect(config.locator('input[name="key"][value="dns.nameservers"]')).toBeVisible();

  // Delete the record, then the zone.
  await expect(async () => {
    await main.getByRole("row", { name: /www/ }).getByRole("button", { name: "Delete" }).click();
    await expect(main.getByText("A 10.0.3.99")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  await main.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page).toHaveURL(/\/network-zones$/);
  await expect(main.getByRole("link", { name: "e2e.example.org" })).toHaveCount(0);
});
