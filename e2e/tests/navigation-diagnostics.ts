import type { BrowserContext, Page, Request } from '@playwright/test'

// Firefox (Playwright 1.63, Juggler) sometimes never reports a navigation that
// the browser has finished: the server answers 200, `document.readyState` is
// "complete" at the new address, but the driver's frame stays on about:blank,
// so `page.goto` waits until its timeout and no locator can see the page.
// Measured with a bare context and page, no fixtures, no route, no init script,
// against this app and its API reference page, 3 to 12 of 40 navigations on a
// 2-CPU Linux container (arm64 and amd64), none against JSON or SVG responses;
// the protocol log shows `goto` followed by request events but never `navigated`.
// A second `goto` to the same address re-syncs the frame, so Firefox contexts
// retry the navigation once or twice with a short timeout. Chromium and WebKit
// are never retried.
const FIREFOX_ATTEMPT_MS = 15_000
const FIREFOX_ATTEMPTS = 3
const FIREFOX_TOTAL_MS = 60_000

// A navigation that never completes says nothing about why. Each context
// remembers the requests that have started and not finished, and a goto that
// fails names them, so the report shows what the page was still waiting for.
function pendingRequests(context: BrowserContext): () => string[] {
  const pending = new Map<Request, number>()
  const done = (request: Request) => { pending.delete(request) }
  context.on('request', (request) => { pending.set(request, Date.now()) })
  context.on('requestfinished', done)
  context.on('requestfailed', done)
  return () => [...pending].map(([request, at]) => `${request.method()} ${request.url()} (${request.resourceType()}, pending ${Date.now() - at} ms)`)
}

type Goto = Page['goto']

function firefoxGoto(goto: Goto): Goto {
  return async (url, options) => {
    const total = options?.timeout || FIREFOX_TOTAL_MS
    const started = Date.now()
    for (let attempt = 1; ; attempt++) {
      const left = Math.max(total - (Date.now() - started), 1)
      try {
        return await goto(url, { ...options, timeout: attempt === FIREFOX_ATTEMPTS ? left : Math.min(FIREFOX_ATTEMPT_MS, left) })
      } catch (err) {
        if (!(err instanceof Error && err.name === 'TimeoutError') || attempt === FIREFOX_ATTEMPTS) throw err
      }
    }
  }
}

/** Makes `page.goto` failures of this context's pages list the requests still open; Firefox also gets the retry described above. */
export function explainNavigationFailures(context: BrowserContext): void {
  const open = pendingRequests(context)
  const firefox = context.browser()?.browserType().name() === 'firefox'
  const newPage = context.newPage.bind(context)
  context.newPage = async (): Promise<Page> => {
    const page = await newPage()
    const plain = page.goto.bind(page)
    const goto = firefox ? firefoxGoto(plain) : plain
    page.goto = async (...args: Parameters<Page['goto']>) => {
      try {
        return await goto(...args)
      } catch (err) {
        const stuck = open()
        // Whether the page itself finished loading although the navigation never reported it.
        const state = await page.evaluate(() => `${document.readyState}, ${location.href}`).catch((e: unknown) => `unreadable (${String(e).slice(0, 80)})`)
        const detail = `\nThe page says: ${state}\nRequests still open in this context (${stuck.length}):\n${stuck.join('\n')}`
        throw new Error(`${err instanceof Error ? err.message : String(err)}${detail}`, { cause: err })
      }
    }
    return page
  }
}
