import { test, expect } from "@playwright/test";

// The interactive console is the one surface unit and Go integration tests
// cannot cover: the browser-side xterm.js wiring (keystrokes -> binary stdin
// frames, binary stdout frames -> rendered cells). The fake backend echoes
// stdin to stdout, so typing a unique string and seeing it rendered proves the
// whole keyboard -> WebSocket -> exec -> stdout -> xterm round-trip end to end.
test("console terminal round-trips typed input over the websocket", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (err) => errors.push(err.message));

  await page.goto("/instances/demo/console");
  await expect(page).toHaveTitle(/demo · console/);

  // xterm has mounted once it renders its row container.
  const terminal = page.locator("#terminal");
  await expect(terminal.locator(".xterm-rows")).toBeVisible();

  // Type a unique command into xterm's hidden input; each keystroke flows
  // through term.onData -> ws.send(binary).
  const marker = "lexi-e2e-ping";
  const input = page.locator("textarea.xterm-helper-textarea");
  await input.focus();
  await input.pressSequentially(marker);
  await input.press("Enter");

  // The fake echoes the bytes back as binary stdout frames, which xterm renders.
  await expect(terminal).toContainText(marker, { timeout: 5_000 });

  expect(errors, "no uncaught page errors").toEqual([]);
});

test("console page keeps the instance tab bar for navigation", async ({ page }) => {
  await page.goto("/instances/demo/console");

  // The instance name and status are shown above the tabs, as on the detail page.
  await expect(page.getByRole("heading", { name: "demo" })).toBeVisible();
  await expect(page.getByRole("main").getByText("Stopped")).toBeVisible();

  // The full set of instance tabs is present, with Console highlighted.
  const console = page.getByRole("link", { name: "Console" });
  await expect(console).toBeVisible();
  await expect(console).toHaveClass(/border-primary/);

  // Tabs are full-page navigations here, so clicking one leaves the console.
  await page.getByRole("link", { name: "Metrics" }).click();
  await expect(page).toHaveURL(/\/instances\/demo\?tab=metrics$/);
  await expect(page.locator("#metrics")).toBeVisible();
});
