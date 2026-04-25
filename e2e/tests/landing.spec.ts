import { test, expect, type ConsoleMessage } from "@playwright/test";

test.describe("Landing Page", () => {
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
    expect(consoleErrors, "unexpected console errors").toEqual([]);
  });

  test("has correct title", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveTitle("ycode Pulse");
  });

  test("grid view is visible by default", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("#grid-home")).toBeVisible();
    await expect(page.locator("#list-view")).toBeHidden();
  });

  test("grid contains app tiles", async ({ page }) => {
    await page.goto("/");
    const tiles = page.locator(".tile");
    await expect(tiles).not.toHaveCount(0);
    const count = await tiles.count();
    expect(count).toBeGreaterThanOrEqual(5);
  });

  test("each tile has icon and label", async ({ page }) => {
    await page.goto("/");
    const tiles = page.locator(".tile");
    const count = await tiles.count();
    for (let i = 0; i < count; i++) {
      const tile = tiles.nth(i);
      await expect(tile.locator(".icon")).toBeVisible();
      await expect(tile.locator(".label")).toBeVisible();
    }
  });

  test("footer has healthz link", async ({ page }) => {
    await page.goto("/");
    const healthLink = page.locator('a[href="/healthz"]');
    await expect(healthLink).toBeVisible();
  });

  test("toggle switches to list view and back", async ({ page }) => {
    await page.goto("/");

    // Switch to list view.
    await page.click("#btn-toggle");
    await expect(page.locator("#list-view")).toBeVisible();
    await expect(page.locator("#grid-home")).toBeHidden();

    // List view has items with data-href.
    const listItems = page.locator(".list-item[data-href]");
    await expect(listItems).not.toHaveCount(0);

    // Switch back to grid view.
    await page.click("#btn-toggle");
    await expect(page.locator("#grid-home")).toBeVisible();
    await expect(page.locator("#list-view")).toBeHidden();
  });

  test("clicking a tile opens iframe", async ({ page }) => {
    await page.goto("/");

    // Click the first tile.
    await page.locator(".tile").first().click();

    // Iframe should appear.
    await expect(page.locator("#grid-frame")).toBeVisible();

    // Home button should appear.
    await expect(page.locator("#btn-home")).toBeVisible();

    // Grid home should be hidden.
    await expect(page.locator("#grid-home")).toBeHidden();
  });

  test("home button returns from iframe to grid", async ({ page }) => {
    await page.goto("/");

    // Open a tile.
    await page.locator(".tile").first().click();
    await expect(page.locator("#grid-frame")).toBeVisible();

    // Click home.
    await page.click("#btn-home");

    // Grid should return, iframe should hide.
    await expect(page.locator("#grid-home")).toBeVisible();
    await expect(page.locator("#grid-frame")).toBeHidden();
  });

  test("list view item selection loads iframe", async ({ page }) => {
    await page.goto("/");

    // Switch to list view.
    await page.click("#btn-toggle");
    await expect(page.locator("#list-view")).toBeVisible();

    // Click first list item.
    const firstItem = page.locator(".list-item").first();
    await firstItem.click();
    await expect(firstItem).toHaveClass(/active/);

    // Iframe should load.
    await expect(page.locator("#list-frame")).toBeVisible();
  });

  test("switching list items updates active class", async ({ page }) => {
    await page.goto("/");

    // Switch to list view.
    await page.click("#btn-toggle");

    const items = page.locator(".list-item");
    const count = await items.count();
    if (count < 2) {
      test.skip();
      return;
    }

    // Click first item.
    await items.nth(0).click();
    await expect(items.nth(0)).toHaveClass(/active/);

    // Click second item.
    await items.nth(1).click();
    await expect(items.nth(1)).toHaveClass(/active/);
    // First should no longer be active.
    await expect(items.nth(0)).not.toHaveClass(/active/);
  });

  test("grid iframe src matches tile href", async ({ page }) => {
    await page.goto("/");

    // Get the onclick attribute of the first tile to extract the href.
    const tile = page.locator(".tile").first();
    const onclick = await tile.getAttribute("onclick");
    // onclick looks like: gridOpen('/prometheus/')
    const match = onclick?.match(/gridOpen\('([^']+)'\)/);
    if (!match) {
      test.skip();
      return;
    }
    const expectedPath = match[1];

    await tile.click();
    const iframe = page.locator("#grid-frame");
    await expect(iframe).toBeVisible();
    const src = await iframe.getAttribute("src");
    expect(src).toBe(expectedPath);
  });
});
