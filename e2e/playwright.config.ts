import { defineConfig, devices } from '@playwright/test'

const PORT = process.env.CAW_PORT || '8080'
const BASE_URL = `http://127.0.0.1:${PORT}`

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  timeout: 300_000,
  expect: { timeout: 30_000 },
  retries: 0,
  globalSetup: './global-setup.ts',
  globalTeardown: './global-teardown.ts',
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 30_000,
    navigationTimeout: 30_000,
  },
  projects: [
    {
      name: 'smoke',
      testDir: './tests/smoke',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'agents',
      testDir: './tests/agents',
      use: { ...devices['Desktop Chrome'] },
      timeout: 300_000,
    },
    {
      name: 'quota',
      testDir: './tests/quota',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})