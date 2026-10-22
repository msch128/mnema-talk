import { test as base, expect } from '@playwright/test'
import type { Browser, BrowserContext } from '@playwright/test'

// Every spec signs in as the Owner from one address, and the API allows 600
// requests a minute per account. Instead of failing (or sleeping) after the
// server answered 429, each test first waits until the browsers of this run
// have sent few enough API requests in the last minute to leave it room.
// Production limits are untouched: this only paces the tests.

const WINDOW_MS = 60_000
// Requests one account may have sent in the previous minute before a test is
// allowed to start; the test itself then still has the rest of the 600.
const HEADROOM = 400
// A fixed window always ends within a minute, so waiting longer is pointless.
const MAX_WAIT_MS = WINDOW_MS + 5_000

// API request times per account (the id inside the session cookie), because
// the limit is per account. Requests without a session count as "anonymous".
const sent = new Map<string, number[]>()

function accountOf(cookie: string | undefined): string {
  const token = /(?:^|;\s*)mnema_session=([^;]+)/.exec(cookie ?? '')?.[1]
  const payload = token?.split('.')[1]
  if (!payload) return 'anonymous'
  try {
    const claims: unknown = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'))
    const uid = typeof claims === 'object' && claims !== null ? Object.getOwnPropertyDescriptor(claims, 'uid')?.value : undefined
    return typeof uid === 'string' ? uid : 'anonymous'
  } catch {
    return 'anonymous'
  }
}

/** The most API requests any one account sent in the last minute. */
function busiestAccount(now = Date.now()): number {
  let busiest = 0
  for (const times of sent.values()) {
    while (times.length > 0 && now - (times[0] ?? now) > WINDOW_MS) times.shift()
    busiest = Math.max(busiest, times.length)
  }
  return busiest
}

/** Waits until no account used more than the headroom of its budget in the last minute. */
export async function awaitApiHeadroom(headroom = HEADROOM): Promise<void> {
  const started = Date.now()
  while (busiestAccount() > headroom && Date.now() - started < MAX_WAIT_MS) {
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
}

function countApiRequests(context: BrowserContext): void {
  context.on('request', (request) => {
    if (!new URL(request.url()).pathname.startsWith('/api/')) return
    const at = Date.now()
    void request.allHeaders().then((headers) => {
      const account = accountOf(headers['cookie'])
      const times = sent.get(account) ?? []
      times.push(at)
      sent.set(account, times)
    }, () => undefined)
  })
}

// Every test registers or signs in accounts that are new to the welcome flow,
// whose dialog would cover the page under test. Contexts therefore see it as
// already finished; the onboarding spec opts back in with showWelcomeFlow.
const ONBOARDING_STATE = '**/api/onboarding'

async function hideWelcomeFlow(context: BrowserContext): Promise<void> {
  await context.route(ONBOARDING_STATE, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    const response = await route.fetch()
    const state: unknown = await response.json().catch(() => null)
    if (typeof state !== 'object' || state === null) return route.fulfill({ response })
    return route.fulfill({ response, json: { ...state, welcome_done: true } })
  })
}

/** Lets the welcome flow appear in this context, as it does for a real new member. */
export async function showWelcomeFlow(context: BrowserContext): Promise<void> {
  await context.unroute(ONBOARDING_STATE)
}

// The server tells clients apart by address (and trusts the loopback proxy's
// X-Forwarded-For), and its sign-in budget of 60 per 15 minutes is per address.
// The whole suite runs from one address, so it sits right at that budget and
// every new sign-in pushes later specs over it. Each browser context therefore
// presents its own (from the benchmarking range, never routed) client address, like
// the separate people it stands for. Production limits are untouched.
let nextClient = 1

function clientAddress(): string {
  const n = nextClient++
  return `198.18.${Math.floor(n / 250) % 250}.${(n % 250) + 1}`
}

function counted(browser: Browser): Browser {
  const newContext = browser.newContext.bind(browser)
  browser.newContext = async (...args: Parameters<Browser['newContext']>) => {
    const options = args[0] ?? {}
    const context = await newContext({ ...options, extraHTTPHeaders: { 'X-Forwarded-For': clientAddress(), ...(options.extraHTTPHeaders ?? {}) } })
    countApiRequests(context)
    await hideWelcomeFlow(context)
    return context
  }
  return browser
}

export const test = base.extend<{ apiHeadroom: void }, { browser: Browser }>({
  browser: [async ({ browser }, use) => { await use(counted(browser)) }, { scope: 'worker' }],
  // Playwright insists on a destructuring pattern, even an empty one.
  // eslint-disable-next-line no-empty-pattern
  apiHeadroom: [async ({}, use) => { await awaitApiHeadroom(); await use() }, { auto: true }],
})

export { expect }
