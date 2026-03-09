import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e/playwright",
  fullyParallel: false,
  timeout: 30000,
  retries: process.env.CI ? 1 : 0,
  use: {
    baseURL: "http://127.0.0.1:4173",
    browserName: "chromium",
    headless: true,
    trace: "on-first-retry",
  },
  webServer: {
    command: "npm run serve:test",
    url: "http://127.0.0.1:4173/healthz",
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
  },
});

