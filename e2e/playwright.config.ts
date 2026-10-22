import { defineConfig } from '@playwright/test'

const baseURL = process.env.E2E_BASE_URL || 'http://127.0.0.1:58080'

// Chromium takes fake media devices as flags. The local harness exercises the
// default route; VM/VPN interfaces whose UDP writes can stall on developer
// machines are excluded unless E2E_WEBRTC_ALL_INTERFACES=1.
const chromiumArgs = [
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
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium', launchOptions: { args: chromiumArgs } },
      // Chromium's sealed-media specs are late-join, key-lifecycle and screen-layers; this
      // one proves the same on the other engines, and the suite's shared
      // sign-in budget (60 per 15 minutes and address) has no room for more.
      testIgnore: /browser-sealing\.spec\.ts$/,
    },
    {
      name: 'webkit',
      use: { browserName: 'webkit', permissions: ['camera', 'microphone'] },
      // Two members of one test run as separate WebKit processes (see fixtures.ts),
      // and the sealed-media specs are timing-sensitive on a loaded machine.
      retries: 1,
    },
    {
      name: 'firefox',
      use: { browserName: 'firefox', launchOptions: { firefoxUserPrefs: firefoxPrefs } },
      retries: 1,
    },
  ],
})
