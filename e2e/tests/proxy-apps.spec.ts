import { test, expect, type ConsoleMessage, type Page } from "@playwright/test";

// Shared console error tracking for each test.
function trackConsoleErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("console", (msg: ConsoleMessage) => {
    if (msg.type() === "error") {
      errors.push(msg.text());
    }
  });
  return errors;
}

test.describe("Prometheus UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = trackConsoleErrors(page);
  });

  test("React app mounts", async ({ page }) => {
    await page.goto("/prometheus/");
    // Wait for React to render content inside #root.
    const root = page.locator("#root");
    await expect(root).not.toBeEmpty();
  });

  test("page title contains Prometheus", async ({ page }) => {
    await page.goto("/prometheus/");
    await expect(page).toHaveTitle(/Prometheus/);
  });

  test("expression input is visible", async ({ page }) => {
    await page.goto("/prometheus/");
    // Prometheus UI has an expression/query input.
    const input = page.locator(
      'textarea, input[type="text"], [class*="expression"], [class*="query"], [class*="input"]'
    );
    await expect(input.first()).toBeVisible({ timeout: 15_000 });
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/prometheus/");
    // Prometheus UI polls continuously, so networkidle never settles.
    // Wait for the React app to mount instead.
    await expect(page.locator("#root")).not.toBeEmpty({ timeout: 10_000 });
    await page.waitForTimeout(2000);
    expect(consoleErrors).toEqual([]);
  });
});

test.describe("Jaeger UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = trackConsoleErrors(page);
  });

  test("app mounts with content", async ({ page }) => {
    await page.goto("/traces/");
    await page.waitForLoadState("networkidle");
    // Jaeger renders a React app; check that the page has meaningful content.
    const body = page.locator("body");
    const text = await body.textContent();
    expect(text).toContain("Jaeger");
  });

  test("search controls render", async ({ page }) => {
    await page.goto("/traces/");
    // Jaeger UI has a service selector and search functionality.
    // Look for any form/select/button related to search.
    const searchArea = page.locator(
      'form, [class*="search"], [class*="Search"], select, [data-testid*="search"]'
    );
    await expect(searchArea.first()).toBeVisible({ timeout: 15_000 });
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/traces/");
    await page.waitForLoadState("networkidle");
    expect(consoleErrors).toEqual([]);
  });
});

test.describe("Perses Dashboards", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = trackConsoleErrors(page);
  });

  test("Vue app mounts and loader disappears", async ({ page }) => {
    await page.goto("/dashboard/");
    // Wait for the loader to disappear and real content to appear.
    const root = page.locator("#root");
    await expect(root).not.toBeEmpty({ timeout: 15_000 });
    // If a loader element exists, wait for it to be gone.
    const loader = page.locator("#loader");
    if ((await loader.count()) > 0) {
      await expect(loader).toBeHidden({ timeout: 15_000 });
    }
  });

  test("contains Perses text", async ({ page }) => {
    await page.goto("/dashboard/");
    await page.waitForLoadState("networkidle");
    const body = page.locator("body");
    const text = await body.textContent();
    expect(text?.toLowerCase()).toContain("perses");
  });

  test("navigation or project list renders", async ({ page }) => {
    await page.goto("/dashboard/");
    // Perses shows navigation items, project list, or dashboard grid.
    const nav = page.locator(
      'nav, [class*="project"], [class*="Project"], [class*="dashboard"], [class*="Dashboard"], a[href*="project"]'
    );
    await expect(nav.first()).toBeVisible({ timeout: 15_000 });
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/dashboard/");
    await page.waitForLoadState("networkidle");
    expect(consoleErrors).toEqual([]);
  });
});

test.describe("Alertmanager UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = trackConsoleErrors(page);
  });

  test("Elm app mounts", async ({ page }) => {
    await page.goto("/alerts/");
    await page.waitForLoadState("networkidle");
    // Alertmanager's Elm app renders main content.
    const body = page.locator("body");
    const text = await body.textContent();
    // Should have some content beyond just script tags.
    expect(text!.trim().length).toBeGreaterThan(0);
  });

  test("alert groups or empty state renders", async ({ page }) => {
    await page.goto("/alerts/");
    // Look for alert group container, empty state, or filter controls.
    const content = page.locator(
      '[class*="alert"], [class*="Alert"], [class*="group"], [class*="filter"], [class*="Filter"], main, #app'
    );
    await expect(content.first()).toBeVisible({ timeout: 15_000 });
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/alerts/");
    await page.waitForLoadState("networkidle");
    expect(consoleErrors).toEqual([]);
  });
});

test.describe("VictoriaLogs UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = trackConsoleErrors(page);
  });

  test("app mounts after redirect", async ({ page }) => {
    // VictoriaLogs redirects / to /select/vmui/.
    await page.goto("/logs/");
    await page.waitForLoadState("networkidle");
    const root = page.locator("#root");
    await expect(root).not.toBeEmpty({ timeout: 15_000 });
  });

  test("query input is visible", async ({ page }) => {
    await page.goto("/logs/");
    await page.waitForLoadState("networkidle");
    // vmui has a query/search input.
    const input = page.locator(
      'textarea, input[type="text"], [class*="query"], [class*="Query"]'
    );
    await expect(input.first()).toBeVisible({ timeout: 15_000 });
  });

  test("contains VictoriaMetrics branding", async ({ page }) => {
    await page.goto("/logs/");
    await page.waitForLoadState("networkidle");
    const body = page.locator("body");
    const text = await body.textContent();
    // vmui shows "victoriametrics" branding in the footer.
    expect(text?.toLowerCase()).toContain("victoriametrics");
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/logs/");
    await page.waitForLoadState("networkidle");
    expect(consoleErrors).toEqual([]);
  });
});

test.describe("Memos UI", () => {
  let consoleErrors: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = [];
    page.on("console", (msg: ConsoleMessage) => {
      if (msg.type() === "error") {
        const text = msg.text();
        // Memos emits expected 401/404 errors for unauthenticated API calls.
        if (text.includes("401") || text.includes("404")) return;
        consoleErrors.push(text);
      }
    });
  });

  test("Vue app mounts", async ({ page }) => {
    await page.goto("/memos/");
    await page.waitForLoadState("networkidle");
    // Memos serves an SPA; wait for main content to render.
    const body = page.locator("body");
    await expect(body).not.toBeEmpty();
    const text = await body.textContent();
    // Should have more than just a loading spinner.
    expect(text!.trim().length).toBeGreaterThan(10);
  });

  test("no CSP violations", async ({ page }) => {
    // Memos has strict CSP headers. Any violation means external requests leaked.
    const cspViolations: string[] = [];
    page.on("console", (msg: ConsoleMessage) => {
      if (msg.text().includes("Content Security Policy")) {
        cspViolations.push(msg.text());
      }
    });
    await page.goto("/memos/");
    await page.waitForLoadState("networkidle");
    expect(cspViolations).toEqual([]);
  });

  test("main content area renders", async ({ page }) => {
    await page.goto("/memos/");
    // Memos renders a sidebar + main content layout.
    const content = page.locator(
      'main, [class*="main"], [class*="content"], [class*="memo"], #root > div'
    );
    await expect(content.first()).toBeVisible({ timeout: 15_000 });
  });

  test("no console errors", async ({ page }) => {
    await page.goto("/memos/");
    await page.waitForLoadState("networkidle");
    expect(consoleErrors).toEqual([]);
  });
});
