import { test, expect } from './fixtures'

// The installable shell: manifest, icons and a service worker that keeps the
// page available offline without ever storing API answers (they carry
// personal data).

interface Manifest {
  name: string
  display: string
  start_url: string
  scope: string
  theme_color: string
  icons: Array<{ src: string, type: string, sizes: string }>
}

test('the app has a manifest with working icons', async ({ page }) => {
  await page.goto('/')
  const href = await page.locator('link[rel="manifest"]').getAttribute('href')
  expect(href).toBe('/manifest.webmanifest')
  const res = await page.request.get('/manifest.webmanifest')
  expect(res.status()).toBe(200)
  expect(res.headers()['content-type']).toContain('application/manifest+json')
  const manifest = await res.json() as Manifest
  expect(manifest).toMatchObject({ name: 'Mnema Talk', display: 'standalone', start_url: '/', scope: '/' })
  for (const icon of manifest.icons) {
    const file = await page.request.get(icon.src)
    expect(file.status(), icon.src).toBe(200)
    expect(file.headers()['content-type'], icon.src).toContain(icon.type)
  }
  expect(manifest.icons.some(i => i.sizes === '192x192')).toBe(true)
  expect(manifest.icons.some(i => i.sizes === '512x512')).toBe(true)
})

test('the service worker serves the shell offline and never stores the API', async ({ page, context }) => {
  await page.goto('/')
  await page.evaluate(async () => { await navigator.serviceWorker.ready })
  // A page is controlled from its next load on.
  await page.reload()
  await expect.poll(() => page.evaluate(() => navigator.serviceWorker.controller !== null)).toBe(true)

  const stored = await page.evaluate(async () => {
    const paths: string[] = []
    for (const name of await caches.keys()) {
      const cache = await caches.open(name)
      for (const request of await cache.keys()) paths.push(new URL(request.url).pathname)
    }
    return paths
  })
  expect(stored).toContain('/')
  expect(stored.some(path => path.startsWith('/assets/'))).toBe(true)
  expect(stored.filter(path => path.startsWith('/api/'))).toEqual([])

  await context.setOffline(true)
  try {
    await page.reload()
    await expect(page.locator('#app > *').first()).toBeVisible()
  } finally {
    await context.setOffline(false)
  }
})

test('the worker script is revalidated on every load', async ({ page }) => {
  const res = await page.request.get('/sw.js')
  expect(res.status()).toBe(200)
  expect(res.headers()['content-type']).toContain('javascript')
  expect(res.headers()['cache-control']).toBe('no-cache')
})
