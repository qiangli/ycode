import { defineConfig } from "@playwright/test";
import { createServer, type AddressInfo } from "net";

// Allocate a random free port for the test server.
// When BASE_URL is set, tests use an external server and skip auto-start.
function findFreePortSync(): number {
  const srv = createServer();
  srv.listen(0, "127.0.0.1");
  const port = (srv.address() as AddressInfo).port;
  srv.close();
  return port;
}

const externalURL = process.env.BASE_URL;
const serverPort = externalURL ? 0 : findFreePortSync();
const baseURL = externalURL || `http://localhost:${serverPort}`;

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  retries: 1,
  workers: 2,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL,
    headless: true,
    screenshot: "only-on-failure",
    video: "on-first-retry",
    actionTimeout: 10_000,
  },
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
  ],
  // Auto-start an isolated ycode serve instance for testing.
  // Skipped when BASE_URL is provided (e.g., testing against a running server).
  ...(externalURL
    ? {}
    : {
        webServer: {
          command: `../bin/ycode serve --port ${serverPort} --no-nats`,
          port: serverPort,
          reuseExistingServer: false,
          timeout: 30_000,
          stdout: "pipe",
          stderr: "pipe",
        },
      }),
});
