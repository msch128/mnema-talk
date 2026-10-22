import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.E2E_BASE_URL || 'http://127.0.0.1:58080'

export default defineConfig({
  testDir: './tests',
  timeout: 90_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  outputDir: 'test-results',
  use: {
    baseURL,
    locale: 'de-DE',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: {
      args: [
        '--use-fake-ui-for-media-stream',
        '--use-fake-device-for-media-stream',
        // The local harness exercises the default route. Exclude additional
        // VM/VPN interfaces whose UDP writes can stall on developer machines.
        ...(process.env.E2E_WEBRTC_ALL_INTERFACES === '1'
          ? []
          : ['--force-webrtc-ip-handling-policy=default_public_and_private_interfaces']),
      ],
    },
  },
  projects: [
    { name: 'chromium', testIgnore: /mobile\.spec\.ts/, use: { browserName: 'chromium' } },
    // Phone layout under device emulation (touch, small screen, safe areas).
    // No service worker: its requests bypass the context routes that keep the
    // welcome flow closed (see fixtures.ts).
    { name: 'mobile-chromium', testMatch: /mobile\.spec\.ts/, use: { ...devices['Pixel 7'], serviceWorkers: 'block', locale: 'de-DE' } },
    { name: 'mobile-webkit', testMatch: /mobile\.spec\.ts/, use: { ...devices['iPhone 15'], serviceWorkers: 'block', locale: 'de-DE', launchOptions: {} } },
  ],
})
