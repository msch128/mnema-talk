import type { BrowserContext, Page, Request } from '@playwright/test'

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

/** Makes `page.goto` failures of this context's pages list the requests still open. */
export function explainNavigationFailures(context: BrowserContext): void {
  const open = pendingRequests(context)
  const newPage = context.newPage.bind(context)
  context.newPage = async (): Promise<Page> => {
    const page = await newPage()
    const goto = page.goto.bind(page)
    page.goto = async (...args: Parameters<Page['goto']>) => {
      try {
        return await goto(...args)
      } catch (err) {
        const stuck = open()
        // Whether the page itself finished loading although the navigation never reported it.
        const state = await page.evaluate(() => `${document.readyState}, ${location.href}`, undefined).catch((e: unknown) => `unreadable (${String(e).slice(0, 80)})`)
        const detail = `\nThe page says: ${state}\nRequests still open in this context (${stuck.length}):\n${stuck.join('\n')}`
        throw new Error(`${err instanceof Error ? err.message : String(err)}${detail}`, { cause: err })
      }
    }
    return page
  }
}
