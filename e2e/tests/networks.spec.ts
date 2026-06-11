import { test, expect } from "@playwright/test";

// Networks: create/delete, managed-network editing, and error toasts.
// All tests run against the shared fake-backed server (instance "demo" seeded).

test("create and delete a network in the Networks section", async ({ page }) => {
  // Network delete asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());
  await page.goto("/");
  await page.getByRole("link", { name: "Networks" }).click();
  await expect(page).toHaveURL(/\/networks$/);
  await expect(page.getByText("incusbr0")).toBeVisible();

  await page.getByRole("link", { name: "Create network" }).click();
  await page.locator('input[name="name"]').fill("e2e-net");
  await page.locator('input[name="key"]').first().fill("ipv4.nat");
  await page.locator('input[name="value"]').first().fill("true");
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page).toHaveURL(/\/networks$/);
  const table = page.locator("#networks-table");
  await expect(table.getByText("e2e-net")).toBeVisible();

  // htmx swap-then-click race: retry the delete until it takes effect (the
  // same pattern as the snapshot/device/volume deletes).
  await expect(async () => {
    await table.getByRole("row", { name: /e2e-net/ }).getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#networks-table").getByText("e2e-net")).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
});

test("edit a managed network's description and config", async ({ page }) => {
  await page.goto("/networks/incusbr0");
  // Scope to the config form: the forwards card has a description input too.
  const config = page.locator('form[action="/networks/incusbr0/config"]');
  await config.locator('input[name="description"]').fill("edited by e2e");
  // Blank key rows are appended after the existing config rows.
  await config.locator('input[name="key"]').last().fill("user.e2e");
  await config.locator('input[name="value"]').last().fill("yes");
  await config.getByRole("button", { name: "Apply config" }).click();

  await expect(page).toHaveURL(/\/networks\/incusbr0$/);
  await expect(config.locator('input[name="description"]')).toHaveValue("edited by e2e");
  await expect(config.locator('input[name="key"][value="user.e2e"]')).toBeVisible();
});

test("backend errors surface as a toast", async ({ page }) => {
  await page.goto("/networks/new");
  // incusbr0 is seeded → creating it again conflicts (409).
  await page.locator('input[name="name"]').fill("incusbr0");
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page.locator("[data-tui-toast]")).toBeVisible();
  // The form is not replaced by the error response, and the failed boosted
  // request must not rewrite history away from the create page.
  await expect(page.locator('input[name="name"]')).toBeVisible();
  await expect(page).toHaveURL(/\/networks\/new$/);
});

test("network ACLs: attach to an instance NIC, in-use guard, detach", async ({ page }) => {
  page.on("dialog", (d) => d.accept());

  // Create the ACL.
  await page.goto("/network-acls");
  await page.locator('input[name="name"]').fill("e2e-nic-acl");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(/\/network-acls\/e2e-nic-acl$/);

  // Attach it via a nic device on the demo instance.
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Devices" }).click();
  const devices = page.locator("#devices");
  const nicForm = devices.locator('details:has-text("Add nic")');
  await nicForm.locator("summary").click();
  await nicForm.locator('input[name="device"]').fill("aclnic");
  await nicForm.locator('input[name="network"]').fill("incusbr0");
  await nicForm.locator('input[name="security.acls"]').fill("e2e-nic-acl");
  await nicForm.getByRole("button", { name: "Add nic" }).click();
  await expect(devices.getByText("aclnic", { exact: true })).toBeVisible();

  // The ACL now reports the attachment; the Delete button is disabled while
  // anything references it.
  await page.goto("/network-acls/e2e-nic-acl");
  await expect(page.getByText(/In use by 1 object/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete", exact: true })).toBeDisabled();

  // Detach (remove the device), then the delete goes through. Scope the
  // Remove click to the aclnic row: reused dev servers can carry leftover
  // devices from interrupted runs, and demo must keep its other devices.
  await page.goto("/instances/demo");
  await page.getByRole("link", { name: "Devices" }).click();
  const aclnicRow = devices.locator("div.flex.items-start", { hasText: "aclnic" });
  await expect(async () => {
    await aclnicRow.getByRole("button", { name: "Remove" }).click();
    await expect(devices.getByText("aclnic", { exact: true })).toHaveCount(0, { timeout: 1000 });
  }).toPass({ timeout: 10000 });

  await page.goto("/network-acls/e2e-nic-acl");
  await expect(async () => {
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(page).toHaveURL(/\/network-acls$/, { timeout: 1000 });
  }).toPass({ timeout: 10000 });
  await expect(page.getByRole("link", { name: "e2e-nic-acl" })).toHaveCount(0);
});

test("network ACLs: create, add rule, remove rule, rename, delete", async ({ page }) => {
  page.on("dialog", (d) => d.accept());
  await page.goto("/networks");
  await page.getByRole("link", { name: "ACLs" }).click();
  await expect(page).toHaveURL(/\/network-acls$/);

  // Create an ACL and land on its detail page.
  await page.locator('input[name="name"]').fill("e2e-acl");
  await page.locator('input[name="description"]').fill("made by e2e");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(/\/network-acls\/e2e-acl$/);

  // Add an ingress rule; the table shows it after the redirect.
  const ingress = page.locator("section", { hasText: "Ingress rules" }).first();
  await ingress.locator('select[name="protocol"]').selectOption("tcp");
  await ingress.locator('input[name="destination_port"]').fill("443");
  await ingress.getByRole("button", { name: "Add rule" }).click();
  await expect(page).toHaveURL(/\/network-acls\/e2e-acl$/);
  await expect(page.getByRole("cell", { name: "allow" })).toBeVisible();

  // Remove the rule again.
  await page.getByRole("button", { name: "Remove" }).click();
  await expect(page.getByRole("cell", { name: "allow" })).toHaveCount(0);

  // Rename, then delete from the renamed detail page.
  await page.locator('input[name="new_name"]').fill("e2e-acl2");
  await page.getByRole("button", { name: "Rename" }).click();
  await expect(page).toHaveURL(/\/network-acls\/e2e-acl2$/);
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page).toHaveURL(/\/network-acls$/);
  await expect(page.getByRole("link", { name: "e2e-acl2" })).toHaveCount(0);
});

test("network detail: leases and port forwards on the managed bridge", async ({ page }) => {
  // Forward delete asks via hx-confirm; accept dialogs.
  page.on("dialog", (d) => d.accept());

  // A running instance holds a DHCP lease on the default bridge.
  await page.goto("/");
  const demoRow = page.locator("#instance-demo");
  const startBtn = demoRow.getByRole("button", { name: "Start", exact: true });
  if (await startBtn.isVisible()) {
    await startBtn.click();
  }
  await page.goto("/networks/incusbr0");
  await expect(page.getByText("MTU 1500")).toBeVisible();
  await expect(page.getByRole("row", { name: /demo/ })).toBeVisible();

  // Create a forward, add a port mapping, then delete it.
  await page.locator('input[name="listen_address"]').fill("192.0.2.40");
  await page.locator('form[action="/networks/incusbr0/forwards"] input[name="description"]').fill("e2e fwd");
  await page.getByRole("button", { name: "Add forward" }).click();
  await expect(page).toHaveURL(/\/networks\/incusbr0$/);
  await expect(page.getByText("192.0.2.40")).toBeVisible();

  const editor = page.locator('form[action="/networks/incusbr0/forwards/192.0.2.40/update"]');
  await editor.locator('input[name="listen_port"]').last().fill("8080");
  await editor.locator('input[name="port_target"]').last().fill("10.0.3.9");
  await editor.getByRole("button", { name: "Apply forward" }).click();
  await expect(page.locator('input[name="listen_port"][value="8080"]')).toBeVisible();

  await page.getByRole("button", { name: "Delete", exact: true }).first().click();
  await expect(page.getByText("192.0.2.40")).toHaveCount(0);

  // Restore the seeded state for later tests: stop demo again.
  await page.goto("/");
  const stopBtn = page.locator("#instance-demo").getByRole("button", { name: "Stop", exact: true });
  if (await stopBtn.isVisible()) {
    await stopBtn.click();
    await expect(page.locator("#instance-demo").getByText("Stopped")).toBeVisible();
  }
});
