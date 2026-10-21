import { isTauri } from '@tauri-apps/api/core'

/**
 * The trusted desktop client runs a signed bundle of this app from its own
 * origin instead of the server's JavaScript (doc/desktop-trusted-client.md).
 * The host announces the bundle it verified before any page script runs. In a
 * browser, or on any other origin, none of this exists.
 */
export type TrustedAppRole = 'selector' | 'instance' | 'surface'
export interface TrustedApp {
  /** Version of the verified bundle, X.Y.Z. */
  version: string
  /** Built into the installer or a verified download. */
  source: 'builtin' | 'cache'
  webClientApi: number
  /** First 8 bytes of SHA-256 over the public key that signed it, hex. */
  keyId: string
  role: TrustedAppRole
}

const VERSION = /^\d{1,9}\.\d{1,9}\.\d{1,9}$/
const KEY_ID = /^[0-9a-f]{16}$/
const ROLES: readonly string[] = ['selector', 'instance', 'surface']
const SOURCES: readonly string[] = ['builtin', 'cache']

/** The host serves the app at mnema://localhost (https://mnema.localhost on Windows). */
function localAppOrigin(): boolean {
  const { protocol, hostname, port } = window.location
  return !port && (protocol === 'mnema:' && hostname === 'localhost' || protocol === 'https:' && hostname === 'mnema.localhost')
}

function isValidOrigin(): boolean {
  if (typeof window === 'undefined' || !isTauri()) return false
  return localAppOrigin()
}

function field(value: object, name: string): unknown {
  return Reflect.get(value, name)
}

function parseVersion(val: unknown): string | null {
  return typeof val === 'string' && VERSION.test(val) ? val : null
}

function parseSource(val: unknown): TrustedApp['source'] | null {
  return typeof val === 'string' && SOURCES.includes(val) ? (val as TrustedApp['source']) : null
}

function parseWebClientApi(val: unknown): number | null {
  return typeof val === 'number' && Number.isSafeInteger(val) && val >= 1 ? val : null
}

function parseKeyId(val: unknown): string | null {
  return typeof val === 'string' && KEY_ID.test(val) ? val : null
}

function parseRole(val: unknown): TrustedAppRole | null {
  return typeof val === 'string' && ROLES.includes(val) ? (val as TrustedAppRole) : null
}

function parseRawIdentity(raw: object): TrustedApp | null {
  const version = parseVersion(field(raw, 'version'))
  if (!version) return null
  const source = parseSource(field(raw, 'source'))
  if (!source) return null
  const webClientApi = parseWebClientApi(field(raw, 'webClientApi'))
  if (webClientApi === null) return null
  const keyId = parseKeyId(field(raw, 'keyId'))
  if (!keyId) return null
  const role = parseRole(field(raw, 'role'))
  if (!role) return null

  return Object.freeze({ version, source, webClientApi, keyId, role })
}

/** The verified bundle this page runs from, or null (browser, wrong origin, malformed). */
export function trustedApp(): TrustedApp | null {
  if (!isValidOrigin()) return null
  const raw: unknown = Reflect.get(window, '__MNEMA_TRUSTED_APP__')
  if (typeof raw !== 'object' || raw === null) return null
  return parseRawIdentity(raw)
}

/** True inside the trusted desktop client. Requests still use the browser transport. */
export function isTrustedDesktop(): boolean {
  return trustedApp() !== null
}
