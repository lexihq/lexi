import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The Server section: trusted certificates, warnings, overview, and config.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("server section: add then remove a trusted certificate", async ({ page }) => {
  // The certificate delete button asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());
  const pem = readFileSync(join(__dirname, "..", "fixtures", "client.pem"), "utf8");
  await page.goto("/server");

  await page.locator('form[action="/server/certificates"] input[name="name"]').fill("e2e-cert");
  await page.locator('form[action="/server/certificates"] select[name="type"]').selectOption("metrics");
  await page.locator('textarea[name="certificate"]').fill(pem);
  await page.getByRole("button", { name: "Add certificate" }).click();

  // Redirects back to /server with the new cert listed. On a reused dev
  // server the duplicate add 409-toasts instead, but the row still exists.
  await expect(page.getByRole("cell", { name: "e2e-cert" })).toBeVisible();

  // Remove it: the row's Delete button swaps the #certificates table in place.
  await expect(async () => {
    await page.locator("#certificates").getByRole("row", { name: /e2e-cert/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#certificates").getByText("e2e-cert")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("server section: edit a trusted certificate (rename + restrict)", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  const pem = readFileSync(join(__dirname, "..", "fixtures", "cert-edit.pem"), "utf8");
  await page.goto("/server");

  // Add a dedicated cert to edit. On a reused dev server the duplicate add
  // 409-toasts instead, but a row matching /e2e-cert-edit/ still exists
  // (possibly already renamed by an earlier run).
  await page.locator('form[action="/server/certificates"] input[name="name"]').fill("e2e-cert-edit");
  await page.locator('form[action="/server/certificates"] select[name="type"]').selectOption("client");
  await page.locator('textarea[name="certificate"]').fill(pem);
  await page.getByRole("button", { name: "Add certificate" }).click();

  const certs = page.locator("#certificates");
  const row = certs.getByRole("row", { name: /e2e-cert-edit/ });
  await expect(row).toBeVisible();

  // Open the row's Edit form, rename and restrict to a project, and save:
  // the #certificates table swaps in place with the new name and badge.
  await row.getByText("Edit", { exact: true }).click();
  await row.locator('input[name="name"]').fill("e2e-cert-edited");
  await row.locator('input[name="restricted"]').check();
  await row.locator('input[name="projects"]').fill("default");
  await row.getByRole("button", { name: "Save" }).click();

  const edited = certs.getByRole("row", { name: /e2e-cert-edited/ });
  await expect(edited).toBeVisible();
  await expect(edited.getByText("restricted")).toBeVisible();

  // Clean up so reruns against a reused dev server start from a known state.
  await expect(async () => {
    await certs.getByRole("row", { name: /e2e-cert-edit/ }).getByRole("button", { name: "Delete" }).click();
    await expect(certs.getByText("e2e-cert-edit")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("server section: hardware inventory lists GPUs, NICs, and disks", async ({ page }) => {
  await page.goto("/server");
  const hardware = page.locator("section", { has: page.getByRole("heading", { name: "Hardware" }) });
  await expect(hardware.getByRole("heading", { name: "Hardware" })).toBeVisible();

  // The fake backend reports a static inventory: one GPU, one NIC with a
  // port, and two disks (one removable).
  await expect(hardware.getByRole("cell", { name: "FakeGPU 1000" })).toBeVisible();
  await expect(hardware.getByRole("cell", { name: "eth0 (00:16:3e:00:00:01)" })).toBeVisible();
  await expect(hardware.getByRole("cell", { name: "nvme0n1" })).toBeVisible();
  await expect(hardware.getByRole("row", { name: /FAKE USB 64/ }).getByText("removable")).toBeVisible();
});

test("server section: acknowledge a warning", async ({ page }) => {
  await page.goto("/server");
  const warnings = page.locator("#warnings");
  const row = warnings.getByRole("row", { name: /CGroup network priority/ });
  await expect(row).toBeVisible();

  // Fresh server: the seeded warning is "new" — acknowledge it. Reused dev
  // server: it may already be acknowledged (button hidden); same final state.
  if (await row.getByRole("button", { name: "Acknowledge" }).isVisible()) {
    await expect(async () => {
      await row.getByRole("button", { name: "Acknowledge" }).click();
      await expect(
        warnings.getByRole("row", { name: /CGroup network priority/ }).getByText("acknowledged"),
      ).toBeVisible({ timeout: 1000 });
    }).toPass({ timeout: 10000 });
  }
  await expect(
    warnings.getByRole("row", { name: /CGroup network priority/ }).getByText("acknowledged"),
  ).toBeVisible();
  await expect(
    warnings.getByRole("row", { name: /CGroup network priority/ }).getByRole("button", { name: "Acknowledge" }),
  ).toHaveCount(0);
});

test("server section: overview, config apply, warning delete", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Server", exact: true }).click();
  await expect(page).toHaveURL(/\/server$/);

  // Overview + seeded config row (config keys render as input values).
  await expect(page.getByText("6.0-fake")).toBeVisible();
  await expect(page.locator('input[name="key"]').first()).toHaveValue("core.https_address");

  // Add a user.* key in the first blank row and apply (plain form + redirect).
  await page.locator('input[name="key"]').nth(1).fill("user.e2e");
  await page.locator('textarea[name="value"]').nth(1).fill("yes");
  await page.getByRole("button", { name: "Apply config" }).click();
  await expect(page).toHaveURL(/\/server$/);
  await expect(page.locator('input[value="user.e2e"]')).toBeVisible();

  // Delete a seeded warning; the table re-renders in place.
  const warnings = page.locator("#warnings");
  await expect(warnings.getByText("KVM support is missing")).toBeVisible();
  await expect(async () => {
    await warnings.getByRole("row", { name: /KVM support/ }).getByRole("button", { name: "Delete" }).click();
    await expect(warnings.getByText("KVM support is missing")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
