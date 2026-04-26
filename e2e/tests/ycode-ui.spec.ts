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

  test("model button shows model name", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    // Wait for status fetch to populate model name.
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });
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

  test("model dropdown opens on click", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });

    // Dropdown should be hidden initially.
    const dropdown = page.locator("#model-dropdown");
    await expect(dropdown).toBeHidden();

    // Click the model button to open dropdown.
    await modelBtn.click();
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Filter input should be visible and focused.
    const filterInput = page.locator("#model-filter");
    await expect(filterInput).toBeVisible();
  });

  test("model dropdown lists available models", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });

    await modelBtn.click();
    const dropdown = page.locator("#model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Should have at least one model item (builtin aliases at minimum).
    const items = page.locator("#model-list li");
    await expect(items.first()).toBeVisible({ timeout: 5_000 });
    const count = await items.count();
    expect(count).toBeGreaterThan(0);
  });

  test("model dropdown filters on typing", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });

    await modelBtn.click();
    const dropdown = page.locator("#model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    const items = page.locator("#model-list li");
    const initialCount = await items.count();

    // Type a filter string that should narrow results.
    const filterInput = page.locator("#model-filter");
    await filterInput.fill("claude");
    await page.waitForTimeout(200);

    const filteredCount = await items.count();
    // Filtered count should be less than or equal to initial (unless all match).
    expect(filteredCount).toBeLessThanOrEqual(initialCount);
    expect(filteredCount).toBeGreaterThan(0);
  });

  test("model dropdown closes on escape", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });

    await modelBtn.click();
    const dropdown = page.locator("#model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Press Escape to close.
    await page.locator("#model-filter").press("Escape");
    await expect(dropdown).toBeHidden();
  });

  test("model dropdown closes on outside click", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });

    await modelBtn.click();
    const dropdown = page.locator("#model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Click outside the dropdown.
    await page.locator("#messages").click();
    await expect(dropdown).toBeHidden();
  });

  test("selecting a model updates the button label", async ({ page }) => {
    await page.goto("/ycode/");
    const modelBtn = page.locator("#model-btn");
    await expect(modelBtn).not.toBeEmpty({ timeout: 10_000 });

    const originalModel = await modelBtn.textContent();

    await modelBtn.click();
    const dropdown = page.locator("#model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Click the first model item that is NOT the current one.
    const items = page.locator("#model-list li:not(.active)");
    const firstNonActive = items.first();
    if ((await items.count()) > 0) {
      const modelName = await firstNonActive.getAttribute("data-model");
      await firstNonActive.click();

      // Dropdown should close.
      await expect(dropdown).toBeHidden();

      // Button label should update.
      if (modelName && modelName !== originalModel) {
        await expect(modelBtn).toHaveText(modelName, { timeout: 5_000 });
      }
    }
  });

  test("models API returns valid JSON", async ({ page }) => {
    await page.goto("/ycode/");
    const response = await page.request.get("/ycode/api/models");
    expect(response.status()).toBe(200);
    const models = await response.json();
    expect(Array.isArray(models)).toBe(true);
    expect(models.length).toBeGreaterThan(0);

    // Verify structure of first model.
    const first = models[0];
    expect(first).toHaveProperty("id");
    expect(first).toHaveProperty("provider");
    expect(first).toHaveProperty("source");
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
