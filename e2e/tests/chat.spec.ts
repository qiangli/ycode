import { test, expect, type ConsoleMessage } from "@playwright/test";

test.describe("Chat Hub UI", () => {
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
    await page.goto("/chat/");
    await expect(page).toHaveTitle("ycode Chat");
  });

  test("sidebar is visible", async ({ page }) => {
    await page.goto("/chat/");
    await expect(page.locator("#sidebar")).toBeVisible();
  });

  test("chat area is visible", async ({ page }) => {
    await page.goto("/chat/");
    await expect(page.locator("#chat-area")).toBeVisible();
  });

  test("connection status shows connected after room selection", async ({
    page,
  }) => {
    await page.goto("/chat/");

    // WebSocket only connects when a room is selected.
    // Create a room first.
    page.on("dialog", async (dialog) => {
      await dialog.accept("e2e-status-room");
    });
    await page.click("#new-room-btn");
    await page.locator("#room-list li").first().click();

    const statusDot = page.locator("#connection-status");
    await expect(statusDot).toHaveClass(/connected/, { timeout: 10_000 });
    const statusText = page.locator("#status-text");
    await expect(statusText).toHaveText("Connected");
  });

  test("room list is visible", async ({ page }) => {
    await page.goto("/chat/");
    await expect(page.locator("#room-list")).toBeVisible();
  });

  test("new room button exists", async ({ page }) => {
    await page.goto("/chat/");
    await expect(page.locator("#new-room-btn")).toBeVisible();
  });

  test("creating a room adds it to the list", async ({ page }) => {
    await page.goto("/chat/");
    // Wait for connection.
    await expect(page.locator("#connection-status")).toHaveClass(/connected/, {
      timeout: 10_000,
    });

    // Handle the prompt dialog that appears when clicking new room.
    page.on("dialog", async (dialog) => {
      await dialog.accept("e2e-test-room");
    });

    const initialCount = await page.locator("#room-list li").count();
    await page.click("#new-room-btn");

    // Wait for new room to appear.
    await expect(page.locator("#room-list li")).toHaveCount(initialCount + 1, {
      timeout: 5_000,
    });
  });

  test("selecting a room activates it", async ({ page }) => {
    await page.goto("/chat/");
    await expect(page.locator("#connection-status")).toHaveClass(/connected/, {
      timeout: 10_000,
    });

    // Create a room if none exist.
    const roomCount = await page.locator("#room-list li").count();
    if (roomCount === 0) {
      page.on("dialog", async (dialog) => {
        await dialog.accept("e2e-select-room");
      });
      await page.click("#new-room-btn");
      await expect(page.locator("#room-list li")).toHaveCount(1, {
        timeout: 5_000,
      });
    }

    // Click the first room.
    const firstRoom = page.locator("#room-list li").first();
    await firstRoom.click();
    await expect(firstRoom).toHaveClass(/active/);

    // Room name should show in header.
    const roomName = page.locator("#room-name");
    await expect(roomName).not.toBeEmpty();

    // Message form should be visible.
    await expect(page.locator("#message-form")).toBeVisible();
  });

  test("message input accepts text when room is selected", async ({
    page,
  }) => {
    await page.goto("/chat/");
    await expect(page.locator("#connection-status")).toHaveClass(/connected/, {
      timeout: 10_000,
    });

    // Create and select a room.
    page.on("dialog", async (dialog) => {
      await dialog.accept("e2e-input-room");
    });
    await page.click("#new-room-btn");
    await page.locator("#room-list li").first().click();

    // Type in message input.
    const input = page.locator("#message-input");
    await expect(input).toBeVisible();
    await input.fill("hello from playwright e2e");
    await expect(input).toHaveValue("hello from playwright e2e");
  });

  test("dashboard overlay opens and closes", async ({ page }) => {
    await page.goto("/chat/");
    await expect(page.locator("#connection-status")).toHaveClass(/connected/, {
      timeout: 10_000,
    });

    // Open dashboard.
    await page.click("#dashboard-btn");
    const dashboard = page.locator("#dashboard");
    await expect(dashboard).toBeVisible();

    // Dashboard sections exist.
    await expect(page.locator("#dash-channels")).toBeVisible();
    await expect(page.locator("#dash-rooms")).toBeVisible();

    // Close dashboard.
    await page.click("#dash-close");
    await expect(dashboard).toBeHidden();
  });

  test("model selector is visible in chat header", async ({ page }) => {
    await page.goto("/chat/");
    const modelBtn = page.locator("#chat-model-btn");
    await expect(modelBtn).toBeAttached();
  });

  test("chat model dropdown opens on click", async ({ page }) => {
    await page.goto("/chat/");

    const modelBtn = page.locator("#chat-model-btn");
    await expect(modelBtn).toBeAttached();

    // Dropdown should be hidden initially.
    const dropdown = page.locator("#chat-model-dropdown");
    await expect(dropdown).toBeHidden();

    // Click the model button to open dropdown.
    await modelBtn.click();
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Filter input should be visible.
    const filterInput = page.locator("#chat-model-filter");
    await expect(filterInput).toBeVisible();
  });

  test("chat model dropdown lists models from API", async ({ page }) => {
    await page.goto("/chat/");

    const modelBtn = page.locator("#chat-model-btn");
    await modelBtn.click();

    const dropdown = page.locator("#chat-model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    // Should have model items (at least builtins).
    const items = page.locator("#chat-model-list li");
    await expect(items.first()).toBeVisible({ timeout: 5_000 });
    const count = await items.count();
    expect(count).toBeGreaterThan(0);
  });

  test("chat model dropdown closes on escape", async ({ page }) => {
    await page.goto("/chat/");

    const modelBtn = page.locator("#chat-model-btn");
    await modelBtn.click();

    const dropdown = page.locator("#chat-model-dropdown");
    await expect(dropdown).toBeVisible({ timeout: 5_000 });

    await page.locator("#chat-model-filter").press("Escape");
    await expect(dropdown).toBeHidden();
  });

  test("chat models API returns valid JSON", async ({ page }) => {
    await page.goto("/chat/");
    const response = await page.request.get("/chat/api/models");
    expect(response.status()).toBe(200);
    const models = await response.json();
    expect(Array.isArray(models)).toBe(true);
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/chat/");
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(2000);
    expect(consoleErrors).toEqual([]);
  });
});
