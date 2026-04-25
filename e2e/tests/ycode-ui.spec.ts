import { test, expect, type ConsoleMessage } from "@playwright/test";

test.describe("ycode Web UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = [];
    page.on("console", (msg: ConsoleMessage) => {
      if (msg.type() === "error") {
        consoleErrors.push(msg.text());
      }
    });
  });

  test("page loads with correct title", async ({ page }) => {
    await page.goto("/ycode/");
    await expect(page).toHaveTitle("ycode");
  });

  test("header renders", async ({ page }) => {
    await page.goto("/ycode/");
    const header = page.locator("#header");
    await expect(header).toBeVisible();
    const h1 = page.locator("h1");
    await expect(h1).toHaveText("ycode");
  });

  test("welcome message shown on fresh load", async ({ page }) => {
    await page.goto("/ycode/");
    const welcome = page.locator("#welcome");
    await expect(welcome).toBeVisible();
  });

  test("input textarea is visible and focusable", async ({ page }) => {
    await page.goto("/ycode/");
    const input = page.locator("#input");
    await expect(input).toBeVisible();
    await input.focus();
    await expect(input).toBeFocused();
  });

  test("send button is present", async ({ page }) => {
    await page.goto("/ycode/");
    const sendBtn = page.locator("#send-btn");
    await expect(sendBtn).toBeVisible();
  });

  test("status badge connects via WebSocket", async ({ page }) => {
    await page.goto("/ycode/");
    const badge = page.locator("#status-badge");
    // Wait for WebSocket to connect — badge should get "connected" class.
    await expect(badge).toHaveClass(/connected/, { timeout: 10_000 });
  });

  test("model label shows model name", async ({ page }) => {
    await page.goto("/ycode/");
    const modelLabel = page.locator("#model-label");
    // Wait for status fetch to populate model name.
    await expect(modelLabel).not.toBeEmpty({ timeout: 10_000 });
  });

  test("typing in input updates textarea value", async ({ page }) => {
    await page.goto("/ycode/");
    const input = page.locator("#input");
    await input.fill("hello from playwright");
    await expect(input).toHaveValue("hello from playwright");
  });

  test("token counter element exists", async ({ page }) => {
    await page.goto("/ycode/");
    const tokenCount = page.locator("#token-count");
    await expect(tokenCount).toBeAttached();
  });

  test("no console errors on load", async ({ page }) => {
    await page.goto("/ycode/");
    // Wait for WebSocket connection and initial API calls.
    await page.waitForLoadState("networkidle");
    // Allow brief delay for async errors.
    await page.waitForTimeout(2000);
    expect(consoleErrors).toEqual([]);
  });
});
