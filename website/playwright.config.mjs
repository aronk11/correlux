import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "tests",
  fullyParallel: true,
  use: {
    baseURL: "http://127.0.0.1:4173/correlux/",
    launchOptions: process.env.CHROME_PATH
      ? { executablePath: process.env.CHROME_PATH }
      : {},
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
    {
      name: "mobile",
      use: { ...devices["iPhone 13"], defaultBrowserType: "chromium" },
    },
  ],
  webServer: {
    command: "npm run preview",
    url: "http://127.0.0.1:4173/correlux/",
    reuseExistingServer: !process.env.CI,
  },
});
