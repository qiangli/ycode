import { test, expect, type ConsoleMessage } from "@playwright/test";

// Verifies that the web UI receives and renders slash-command stream events
// (command.progress / command.delta / command.complete). Before the fix,
// these events arrived on the bus but had no handler in the frontend, so a
// /init invocation showed nothing and never re-enabled the send button.
test.describe("/init slash command streaming", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = [];
    page.on("console", (msg: ConsoleMessage) => {
      if (msg.type() === "error") {
        consoleErrors.push(msg.text());
      }
    });
  });

  test("init produces progress output and re-enables input", async ({
    page,
  }) => {
    await page.goto("/ycode/");

    // Wait for WebSocket to connect.
    await expect(page.locator("#status-badge")).toHaveClass(/connected/, {
      timeout: 10_000,
    });

    // Submit /init.
    const input = page.locator("#input");
    const sendBtn = page.locator("#send-btn");
    await input.fill("/init");
    await sendBtn.click();

    // The deterministic scaffold phase emits at least one progress line
    // before any LLM call. We don't assert specific content because /init's
    // output depends on the cwd of the server; we just need to see *some*
    // streamed text appear in the messages container.
    const messages = page.locator("#messages");
    await expect(messages).toContainText(/[⧗✓✗\w]/, { timeout: 15_000 });

    // The command must eventually complete: send button re-enables. If the
    // command.complete event isn't handled, the input stays locked forever
    // (this was the original symptom).
    await expect(sendBtn).toBeEnabled({ timeout: 30_000 });

    expect(consoleErrors).toEqual([]);
  });
});
