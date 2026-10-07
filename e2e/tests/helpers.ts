import type { Page, Locator } from '@playwright/test'
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
  const result = await page.evaluate(async ({ method, path, body }) => {
    const response = await fetch(path, {
      method, credentials: 'same-origin',
      ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    })
    const json: unknown = await response.json().catch(() => null)
    return { status: response.status, json }
  }, { method, path, body })
  if (method === 'GET' && path === '/api/channels') {
    if (result.status !== 200) throw new Error(`Expected channel hierarchy, received HTTP ${result.status}`)
    return { ...result, json: decodeChatChannelHierarchy(result.json) }
  }
  return result
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
