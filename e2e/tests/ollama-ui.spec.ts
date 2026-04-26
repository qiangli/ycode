import { test, expect, type ConsoleMessage } from "@playwright/test";

test.describe("Ollama Management UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = [];
    page.on("console", (msg: ConsoleMessage) => {
      if (msg.type() === "error") {
        consoleErrors.push(msg.text());
      }
    });
  });

  test.afterEach(async () => {
    // Allow fetch errors when Ollama is not running — only fail on app errors.
    const appErrors = consoleErrors.filter(
      (e) => !e.includes("Failed to fetch") && !e.includes("NetworkError")
    );
    expect(appErrors, "unexpected console errors").toEqual([]);
  });

  test("page loads with title", async ({ page }) => {
    await page.goto("/ollama/");
    await expect(page.locator("header h1")).toContainText("Ollama");
  });

  test("status indicator is visible", async ({ page }) => {
    await page.goto("/ollama/");
    await expect(page.locator("#statusDot")).toBeVisible();
    await expect(page.locator("#statusText")).toBeVisible();
  });

  test("nav tabs are present", async ({ page }) => {
    await page.goto("/ollama/");
    const nav = page.locator("nav");
    await expect(nav.getByText("Models")).toBeVisible();
    await expect(nav.getByText("Running")).toBeVisible();
    await expect(nav.getByText("Pull")).toBeVisible();
    await expect(nav.getByText("Chat & Dashboard")).toBeVisible();
  });

  test("models tab is active by default", async ({ page }) => {
    await page.goto("/ollama/");
    const modelsBtn = page.locator("nav button").first();
    await expect(modelsBtn).toHaveClass(/active/);
    await expect(page.locator("#tab-models")).toBeVisible();
    await expect(page.locator("#tab-running")).toBeHidden();
    await expect(page.locator("#tab-pull")).toBeHidden();
    await expect(page.locator("#tab-links")).toBeHidden();
  });

  test("switching to Running tab", async ({ page }) => {
    await page.goto("/ollama/");
    await page.locator("nav").getByText("Running").click();
    await expect(page.locator("#tab-running")).toBeVisible();
    await expect(page.locator("#tab-models")).toBeHidden();
  });

  test("switching to Pull tab", async ({ page }) => {
    await page.goto("/ollama/");
    await page.locator("nav").getByText("Pull").click();
    await expect(page.locator("#tab-pull")).toBeVisible();
    await expect(page.locator("#tab-models")).toBeHidden();
  });

  test("Pull tab has input fields", async ({ page }) => {
    await page.goto("/ollama/");
    await page.locator("nav").getByText("Pull").click();
    await expect(page.locator("#pullInput")).toBeVisible();
    await expect(page.locator("#pullBtn")).toBeVisible();
    await expect(page.locator("#hfInput")).toBeVisible();
  });

  test("switching to Chat & Dashboard tab shows links", async ({ page }) => {
    await page.goto("/ollama/");
    await page.locator("nav").getByText("Chat & Dashboard").click();
    await expect(page.locator("#tab-links")).toBeVisible();

    // Verify integration links.
    await expect(page.locator('a[href="../chat/"]')).toBeVisible();
    await expect(page.locator('a[href="../dashboard/"]')).toBeVisible();
    await expect(page.locator('a[href="../traces/"]')).toBeVisible();
    await expect(page.locator('a[href="../logs/"]')).toBeVisible();
  });

  test("models tab shows model list or empty state", async ({ page }) => {
    await page.goto("/ollama/");
    // Wait for either a table or the empty state message.
    const modelList = page.locator("#modelList");
    await expect(modelList).toBeVisible();
    // Should contain either a table or "No models" or "Loading" or "Failed".
    const text = await modelList.textContent();
    expect(text).toBeTruthy();
  });

  test("Ollama status detects server", async ({ page }) => {
    await page.goto("/ollama/");
    // Wait for status check to complete (up to 5s).
    await expect(page.locator("#statusText")).not.toHaveText("Checking...", {
      timeout: 5_000,
    });
    // Should be either "Running" or "Disconnected".
    const status = await page.locator("#statusText").textContent();
    expect(["Running", "Disconnected"]).toContain(status);
  });

  test("landing page has Ollama tile", async ({ page }) => {
    await page.goto("/");
    const ollamaTile = page.locator('.tile:has(.label:text("Ollama"))');
    await expect(ollamaTile).toBeVisible();
  });

  test("clicking Ollama tile from landing loads UI", async ({ page }) => {
    await page.goto("/");
    // Find and click the Ollama tile.
    const ollamaTile = page.locator('.tile:has(.label:text("Ollama"))');
    await ollamaTile.click();

    // Should open in iframe.
    const iframe = page.locator("#grid-frame");
    await expect(iframe).toBeVisible();
    const src = await iframe.getAttribute("src");
    expect(src).toBe("/ollama/");
  });
});
