import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure'
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'], ...(process.env.CI ? {} : { channel: 'chrome' }) } }
  ],
  webServer: [
    { command: 'python3 -m http.server 4175 --directory tests/upstream', port: 4175, reuseExistingServer: false },
    { command: 'node scripts/reset-e2e-storage.mjs && npm run build && cp tests/e2e.yaml /tmp/reqrelay-phase10-e2e.yaml && go run ../cmd/reqrelay --config /tmp/reqrelay-phase10-e2e.yaml', port: 4173, reuseExistingServer: false, timeout: 120_000 }
  ]
});
