import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.E2E_BASE_URL || 'http://127.0.0.1:58080'

// Chromium takes fake media devices as flags. The local harness exercises the
// default route; VM/VPN interfaces whose UDP writes can stall on developer
// machines are excluded unless E2E_WEBRTC_ALL_INTERFACES=1.
const chromiumArgs = [
  '--mute-audio',
  '--use-fake-ui-for-media-stream',
  '--use-fake-device-for-media-stream',
  ...(process.env.E2E_WEBRTC_ALL_INTERFACES === '1'
    ? []
    : ['--force-webrtc-ip-handling-policy=default_public_and_private_interfaces']),
]

// Firefox takes them as preferences: synthetic devices and no permission prompt.
const firefoxPrefs = {
  'media.navigator.streams.fake': true,
  'media.navigator.permission.disabled': true,
  'media.peerconnection.ice.obfuscate_host_addresses': false,
  // The harness reaches the SFU on 127.0.0.1, which Firefox leaves out by default.
  'media.peerconnection.ice.loopback': true,
}

export default defineConfig({
  testDir: './tests',
  timeout: 90_000,
  // A run that hangs ends with the results of the specs that finished (and
  // their traces) instead of being cancelled by the CI job; 0 = no limit.
  globalTimeout: Number(process.env.E2E_GLOBAL_TIMEOUT_MS ?? 0),
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  outputDir: 'test-results',
  use: {
    baseURL,
    // A page that does not load fails within this, with the requests it waited for (tests/navigation-diagnostics.ts), instead of waiting for the test timeout.
    navigationTimeout: 45_000,
    locale: 'de-DE',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      testIgnore: /mobile\.spec\.ts/,
      use: { browserName: 'chromium', launchOptions: { args: chromiumArgs } },
    },
    {
      name: 'webkit',
      testIgnore: /mobile\.spec\.ts/,
      use: { browserName: 'webkit', permissions: ['camera', 'microphone'] },
      // Two members of one test run as separate WebKit processes (see fixtures.ts),
      // and the sealed-media specs are timing-sensitive on a loaded machine.
      retries: 1,
    },
    {
      name: 'firefox',
      testIgnore: /mobile\.spec\.ts/,
      use: { browserName: 'firefox', launchOptions: { firefoxUserPrefs: firefoxPrefs } },
      retries: 1,
    },
    // Phone layout under device emulation (touch, small screen, safe areas).
    // No service worker: its requests bypass the context routes that keep the
    // welcome flow closed (see fixtures.ts).
    { name: 'mobile-chromium', testMatch: /mobile\.spec\.ts/, use: { ...devices['Pixel 7'], serviceWorkers: 'block', locale: 'de-DE', launchOptions: { args: chromiumArgs } } },
    { name: 'mobile-webkit', testMatch: /mobile\.spec\.ts/, use: { ...devices['iPhone 15'], serviceWorkers: 'block', locale: 'de-DE', permissions: ['camera', 'microphone'] } },
  ],
})
