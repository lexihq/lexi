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
  const marker = "lxcon-e2e-ping";
  const input = page.locator("textarea.xterm-helper-textarea");
  await input.focus();
  await input.pressSequentially(marker);
  await input.press("Enter");

  // The fake echoes the bytes back as binary stdout frames, which xterm renders.
  await expect(terminal).toContainText(marker, { timeout: 5_000 });

  expect(errors, "no uncaught page errors").toEqual([]);
});
