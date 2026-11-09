import { defineConfig, devices } from "@playwright/test";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const e2eRoot = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(e2eRoot, "..");
const appURL = "http://127.0.0.1:18080";
const fakeAgentKURL = "http://127.0.0.1:18081";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  timeout: 20_000,
  expect: {
    timeout: 7_000,
  },
  outputDir: "test-results",
  reporter: [
    ["list"],
    ["html", { outputFolder: "playwright-report", open: "never" }],
  ],
  use: {
    baseURL: appURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: [
    {
      command: "npm run fake-agentk",
      cwd: e2eRoot,
      url: fakeAgentKURL + "/healthz",
      timeout: 30_000,
      reuseExistingServer: false,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      command: "go run ./cmd/apiserver",
      cwd: repositoryRoot,
      env: {
        ...process.env,
        API_HOST: "127.0.0.1",
        API_PORT: "18080",
        DRAMA_MODE: "agent",
        AGENTK_LOCAL_BASE_URL: fakeAgentKURL,
        PROFILE_FILE: resolve(repositoryRoot, "agents/comic-drama/profile-agent.json"),
        COMIC_DRAMA_BLOB_ENDPOINT: fakeAgentKURL + "/storage",
        COMIC_DRAMA_BLOB_BUCKET: "e2e",
        COMIC_DRAMA_BLOB_CDN_BASE: fakeAgentKURL + "/assets/uploads",
      },
      url: appURL + "/healthz",
      timeout: 120_000,
      reuseExistingServer: false,
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
});
