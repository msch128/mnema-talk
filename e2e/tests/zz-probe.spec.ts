import { test } from '@playwright/test'
import type { Browser, BrowserContext } from '@playwright/test'

type Variant = { name: string; url?: string; wait?: 'load' | 'domcontentloaded' | 'commit'; setup: (ctx: BrowserContext) => Promise<void>; options?: Parameters<Browser['newContext']>[0] }

const variants: Variant[] = [
  { name: 'app-dcl', wait: 'domcontentloaded', setup: async () => {} },
  { name: 'app-commit', wait: 'commit', setup: async () => {} },
  { name: 'nojs', setup: async () => {}, options: { javaScriptEnabled: false } },
  { name: 'health', url: '/api/health', setup: async () => {} },
  { name: 'docs', url: '/api/docs', setup: async () => {} },
  { name: 'favicon', url: '/favicon.svg', setup: async () => {} },
  { name: 'plain', setup: async () => {} },
  { name: 'headers', setup: async () => {}, options: { extraHTTPHeaders: { 'X-Forwarded-For': '198.18.9.9' } } },
  { name: 'route', setup: async (c) => { await c.route('**/api/onboarding', r => r.fallback()) } },
  { name: 'binding+init', setup: async (c) => { await c.exposeBinding('__b', () => {}); await c.addInitScript('(()=>{document.addEventListener("securitypolicyviolation",()=>{},true)})()') } },
  { name: 'init-only', setup: async (c) => { await c.addInitScript('(()=>{})()') } },
  { name: 'binding-only', setup: async (c) => { await c.exposeBinding('__b', () => {}) } },
  { name: 'sw-block', setup: async () => {}, options: { serviceWorkers: 'block' } },
  { name: 'request-listener', setup: async (c) => { c.on('request', (r) => { void r.allHeaders().catch(() => {}) }) } },
]

const N = Number(process.env.PROBE_N ?? 8)
const only = process.env.PROBE_ONLY

for (const v of variants) {
  if (only && !only.split(',').includes(v.name)) continue
  test(`probe ${v.name}`, async ({ browser }) => {
    test.setTimeout(600_000)
    let hung = 0
    for (let i = 0; i < N; i++) {
      const ctx = await browser.newContext(v.options)
      await v.setup(ctx)
      const page = await ctx.newPage()
      try {
        await page.goto(v.url ?? '/', { timeout: 15_000, waitUntil: v.wait ?? 'load' })
      } catch {
        hung++
        const usable = await page.getByRole('dialog').waitFor({ timeout: 5000 }).then(() => 'dialog visible', (e: unknown) => 'no dialog ' + String(e).slice(0, 60))
        const url = await page.evaluate(() => location.href).catch(() => 'n/a')
        console.log(`PROBE hang: ${usable}; page.url()=${page.url()}; location=${url}`)
      }
      await ctx.close()
    }
    console.log(`PROBE ${v.name}: ${hung}/${N} navigations hung`)
  })
}
