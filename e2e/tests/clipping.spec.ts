import { writeFileSync } from 'node:fs'
import type { Browser, BrowserContext, Page, TestInfo } from '@playwright/test'
import { test, expect, showWelcomeFlow } from './fixtures'
import { apiFetch } from './helpers'
import { auditClipping, type ClipFinding } from './clipping/auditor'
import { EXCEPTIONS, isExcepted, unexplained } from './clipping/exceptions'
import { translate } from './clipping/i18n'
import { type Combo, comboLabel, cssViewport, fullMatrix, smokeMatrix } from './clipping/matrix'
import { allScenes, type Scene, type SceneContext } from './clipping/scenes'
import { ADMIN_PASSWORD, removeWorld, seedWorld, signInApi, type World } from './clipping/seed'
import { fakeDisplayMedia, routeSocket } from './clipping/talk'

// The visual clipping audit. Every scene (clipping/scenes.ts) at every size,
// language and zoom (clipping/matrix.ts) is checked by clipping/auditor.ts for
// content cut by its box, truncation without the full text, elements partly
// off screen or cut by an ancestor, controls covered by something else, and
// glyphs cut at the top or bottom. A finding fails the test with its scene,
// rule, selector and numbers, and a screenshot of it is attached; deliberate
// cases go on clipping/exceptions.ts with a reason.
//
// The app ships one theme (style.css @theme, color-scheme dark), so there is no
// theme axis. CLIP_SWEEP=full runs the whole matrix (its own CI job, sharded
// like the e2e jobs); without it the smoke subset runs with the other specs.

if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const FULL = process.env.CLIP_SWEEP === 'full'
// CLIP_BROWSER_FONT=20: the browser's default font size (Chromium's setting), as a person who reads larger text sets it.
const BROWSER_FONT = Number(process.env.CLIP_BROWSER_FONT ?? 0)

/** Sets the browser's default font size for the page (Chromium only; other engines keep their default). */
async function browserFont(page: Page) {
  if (!BROWSER_FONT || page.context().browser()?.browserType().name() !== 'chromium') return
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Page.setFontSizes', { fontSizes: { standard: BROWSER_FONT, fixed: Math.round(BROWSER_FONT * 0.8) } })
}
// Each size is a test of its own, so --shard deals the sizes out over the jobs.
test.describe.configure({ mode: 'parallel' })

const COMPACT_BELOW = 768

/** What one scene showed; `unreached` says why the sweep could not get there. */
interface Report { scene: string; findings: ClipFinding[]; unreached?: string }

/** Shows the finding: the element and some room around it, clamped to the window. */
async function shoot(page: Page, info: TestInfo, scene: string, finding: ClipFinding, index: number) {
  const view = page.viewportSize() ?? { width: 1280, height: 720 }
  const pad = 24
  const x = Math.max(0, Math.min(finding.rect.x - pad, view.width - 1))
  const y = Math.max(0, Math.min(finding.rect.y - pad, view.height - 1))
  const width = Math.max(1, Math.min(finding.rect.width + 2 * pad, view.width - x))
  const height = Math.max(1, Math.min(finding.rect.height + 2 * pad, view.height - y))
  await attachShot(page, info, `${scene} - ${finding.rule} ${index}`, { x, y, width, height })
}

/** Saves a screenshot into the test's output folder (a CI artifact) and attaches it to the report. */
async function attachShot(page: Page, info: TestInfo, name: string, clip?: { x: number; y: number; width: number; height: number }) {
  const path = info.outputPath(`${name.replace(/[^a-zA-Z0-9-]+/g, '-')}.png`)
  const done = await page.screenshot({ path, animations: 'disabled', ...(clip ? { clip } : {}) }).then(() => true, () => false)
  if (done) await info.attach(name, { path, contentType: 'image/png' })
}

async function audit(page: Page, info: TestInfo, scene: string, report: Report[]) {
  await page.mouse.move(1, 1)
  await page.waitForTimeout(350) // entry animations settle
  const found = (await page.evaluate(auditClipping, { tolerance: 1 })).filter(f => !isExcepted(scene, f))
  report.push({ scene, findings: found })
  if (found.length === 0) return
  await attachShot(page, info, scene)
  for (const [i, f] of found.slice(0, 12).entries()) await shoot(page, info, scene, f, i)
}

async function reachAndAudit(page: Page, info: TestInfo, scene: Scene, ctx: SceneContext, report: Report[]) {
  const started = Date.now()
  try {
    await reachAndAuditOnce(page, info, scene, ctx, report)
  } finally {
    // Per scene in the CI log, so a slow scene shows up as a number.
    console.log(`[clip] ${comboLabel(ctx.combo)} | ${scene.name} | ${Date.now() - started} ms`)
  }
}

async function reachAndAuditOnce(page: Page, info: TestInfo, scene: Scene, ctx: SceneContext, report: Report[]) {
  try {
    await scene.reach(ctx)
  } catch (err) {
    // A scene that cannot be reached is reported as such and the sweep goes on.
    report.push({ scene: scene.name, findings: [], unreached: String(err).split('\n')[0]?.slice(0, 300) ?? 'unknown' })
    await attachShot(page, info, `${scene.name} not reached`)
    await page.keyboard.press('Escape').catch(() => undefined)
    return
  }
  await audit(page, info, scene.name, report)
  await scene.leave?.(ctx).catch((err: unknown) => console.log(`[clip] ${scene.name}: could not tidy up: ${String(err)}`))
}

/** The sign-in screen and, for a new member, the welcome flow: before anyone is signed in here. */
async function signedOutScenes(browser: Browser, combo: Combo, world: World, info: TestInfo, report: Report[]) {
  const options = { viewport: cssViewport(combo), deviceScaleFactor: combo.zoom, locale: combo.locale === 'en' ? 'en-US' : 'de-DE' }
  const ctx = await browser.newContext(options)
  const page = await ctx.newPage()
  await browserFont(page)
  await page.goto('/')
  await expect(page.getByTestId('login-heading')).toBeVisible()
  await audit(page, info, 'sign-in screen', report)
  await ctx.close()

  const member = await browser.newContext(options)
  await showWelcomeFlow(member)
  const mp = await member.newPage()
  await browserFont(mp)
  await signInApi(mp, world.member.username, world.member.password)
  await apiFetch(mp, 'PUT', '/api/users/me/locale', { locale: combo.locale })
  // The member is new to the welcome flow on every combo, whatever an earlier
  // combo or scene did with it: the server is told the flow is not done.
  expect((await apiFetch(mp, 'PUT', '/api/onboarding/tips', { welcome_done: false })).status).toBe(200)
  await mp.goto('/')
  const welcome = mp.getByRole('dialog')
  // isVisible does not wait (its timeout is ignored): wait for the dialog itself.
  if (await welcome.waitFor({ state: 'visible', timeout: 10_000 }).then(() => true, () => false)) await audit(mp, info, 'welcome flow', report)
  else report.push({ scene: 'welcome flow', findings: [], unreached: 'the welcome flow did not open' })
  await member.close()
}

function describe(report: Report[]): string[] {
  return report.flatMap(r => [
    ...(r.unreached ? [`${r.scene} | not reached | ${r.unreached}`] : []),
    ...r.findings.map(f => `${r.scene} | ${f.rule} | ${f.selector} | "${f.text.slice(0, 50)}" | ${JSON.stringify(f.measured)}`),
  ])
}

async function signedIn(browser: Browser, combo: Combo): Promise<{ ctx: BrowserContext; page: Page }> {
  const ctx = await browser.newContext({
    viewport: cssViewport(combo),
    deviceScaleFactor: combo.zoom,
    locale: combo.locale === 'en' ? 'en-US' : 'de-DE',
    permissions: ['microphone', 'camera'],
  })
  await ctx.addInitScript(fakeDisplayMedia)
  const page = await ctx.newPage()
  await browserFont(page)
  await signInApi(page)
  expect((await apiFetch(page, 'PUT', '/api/users/me/locale', { locale: combo.locale })).status).toBe(200)
  return { ctx, page }
}

test.afterAll(async ({ browser, baseURL }, info) => {
  await removeWorld(browser, baseURL ?? '', info.project.outputDir)
})

test('every clipping exception gives its reason', () => {
  expect(unexplained(), 'exceptions without a reason').toEqual([])
  expect(EXCEPTIONS.length).toBeGreaterThanOrEqual(0)
})

for (const combo of FULL ? fullMatrix() : smokeMatrix()) {
  test(`nothing is clipped: ${comboLabel(combo)}`, async ({ browser, baseURL }, info) => {
    test.setTimeout(FULL ? 20 * 60_000 : 6 * 60_000)
    const world = await seedWorld(browser, baseURL ?? '', info.project.outputDir)
    const report: Report[] = []
    await signedOutScenes(browser, combo, world, info, report)

    const { ctx, page } = await signedIn(browser, combo)
    // A scene whose control is missing fails within this instead of holding the run until the test timeout.
    page.setDefaultTimeout(10_000)
    const socket = await routeSocket(page)
    const scenes = allScenes().filter(s => FULL || s.smoke)
    const context: SceneContext = {
      page, world, combo, socket,
      t: (key, params) => translate(combo.locale, key, params),
      compact: cssViewport(combo).width < COMPACT_BELOW,
    }
    try {
      for (const scene of scenes) await reachAndAudit(page, info, scene, context, report)
    } finally {
      await page.getByTestId('voice-panel-leave').click({ timeout: 2_000 }).catch(() => undefined)
      await apiFetch(page, 'PUT', '/api/users/me/locale', { locale: 'de' }).catch(() => undefined)
      await ctx.close()
    }
    const findings = info.outputPath('clip-findings.json')
    writeFileSync(findings, JSON.stringify({ combo: comboLabel(combo), report }, null, 2))
    await info.attach('clip-findings.json', { path: findings, contentType: 'application/json' })
    expect(describe(report), `clipped in ${comboLabel(combo)}`).toEqual([])
  })
}
