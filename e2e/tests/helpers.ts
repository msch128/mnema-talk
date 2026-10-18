import { randomBytes } from 'node:crypto'
import type { Page, Locator } from '@playwright/test'

// The invite-only sign-up allows ten registrations an hour from one address and
// the whole suite shares them, so specs that need a second member share one.
export const suiteMember = {
  username: `member_${Date.now().toString(36)}`,
  password: `pw-${Date.now().toString(36)}-${randomBytes(8).toString('hex')}`,
  registered: false,
}
import { decodeChatChannelHierarchy } from '../../web/src/types/rest'
import { decodeUser } from '../../web/src/types/domain'
import type { ChatChannelHierarchy } from '../../web/src/types/rest'

export interface ApiResponse<T = unknown> { status: number; json: T }

export async function bounds(locator: Locator): Promise<{ x: number; y: number; width: number; height: number }> {
  const box = await locator.boundingBox()
  if (!box) throw new Error('Expected visible element bounds')
  return box
}

// Preserve browser cookies and Origin checks; validate the returned wire data
// in the harness before a test treats it as a typed application object.
export function apiFetch(page: Page, method: 'GET', path: '/api/channels', body?: unknown): Promise<ApiResponse<ChatChannelHierarchy>>
export function apiFetch(page: Page, method: string, path: string, body?: unknown): Promise<ApiResponse>
export async function apiFetch(page: Page, method: string, path: string, body?: unknown): Promise<ApiResponse> {
  // Every spec signs in as the same administrator, so they share one request
  // budget (600 a minute); a spec that starts inside an exhausted window waits
  // it out instead of failing.
  let result = await fetchOnce(page, method, path, body)
  for (let attempt = 0; attempt < 4 && result.status === 429; attempt++) {
    await page.waitForTimeout(15_000)
    result = await fetchOnce(page, method, path, body)
  }
  if (method === 'GET' && path === '/api/channels') {
    if (result.status !== 200) throw new Error(`Expected channel hierarchy, received HTTP ${result.status}`)
    return { ...result, json: decodeChatChannelHierarchy(result.json) }
  }
  return result
}

function fetchOnce(page: Page, method: string, path: string, body: unknown): Promise<ApiResponse> {
  return page.evaluate(async ({ method, path, body }) => {
    const response = await fetch(path, {
      method, credentials: 'same-origin',
      ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    })
    const json: unknown = await response.json().catch(() => null)
    return { status: response.status, json }
  }, { method, path, body })
}

// The API allows 600 requests a minute per account and every spec signs in as
// the same administrator from one address, so a spec that follows a busy one
// can start inside an exhausted window. Waits out the Retry-After of such a
// 429 and reports whether the page was throttled, so the caller can reload it.
export async function awaitApiBudget(page: Page): Promise<boolean> {
  let throttled = false
  for (let attempt = 0; attempt < 3; attempt++) {
    const retryAfter = await page.evaluate(async () => {
      const response = await fetch('/api/auth/me', { credentials: 'same-origin' })
      return response.status === 429 ? Number(response.headers.get('Retry-After') ?? '5') : 0
    })
    if (retryAfter === 0) break
    throttled = true
    await page.waitForTimeout((Math.min(retryAfter, 65) + 1) * 1000)
  }
  return throttled
}

export function responseString(response: ApiResponse, field: string): string {
  const value = response.json
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Expected an object response')
  const property: unknown = Object.getOwnPropertyDescriptor(value, field)?.value
  if (typeof property !== 'string' || property.length === 0) throw new Error(`Expected a string response field: ${field}`)
  return property
}

export function responseUserId(response: ApiResponse): string {
  // /api/auth/me returns User directly; login/register return UserEnvelope.
  return decodeUser(response.json).id
}

export function trackPeerConnections(): void {
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.RTCPeerConnection = class extends Native {
    constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) {
      super(...args)
      window.__pcs.push(this)
    }
  }
}
