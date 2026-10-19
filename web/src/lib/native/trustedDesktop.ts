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

function field(value: object, name: string): unknown {
  return Reflect.get(value, name)
}

/** The verified bundle this page runs from, or null (browser, wrong origin, malformed). */
export function trustedApp(): TrustedApp | null {
  if (typeof window === 'undefined' || !isTauri() || !localAppOrigin()) return null
  const raw: unknown = Reflect.get(window, '__MNEMA_TRUSTED_APP__')
  if (typeof raw !== 'object' || raw === null) return null
  const version = field(raw, 'version'); const source = field(raw, 'source'); const webClientApi = field(raw, 'webClientApi')
  const keyId = field(raw, 'keyId'); const role = field(raw, 'role')
  if (typeof version !== 'string' || !VERSION.test(version)) return null
  if (typeof source !== 'string' || !SOURCES.includes(source)) return null
  if (typeof webClientApi !== 'number' || !Number.isSafeInteger(webClientApi) || webClientApi < 1) return null
  if (typeof keyId !== 'string' || !KEY_ID.test(keyId)) return null
  if (typeof role !== 'string' || !ROLES.includes(role)) return null
  return Object.freeze({ version, source: source as TrustedApp['source'], webClientApi, keyId, role: role as TrustedAppRole })
}

/** True inside the trusted desktop client. Requests still use the browser transport. */
export function isTrustedDesktop(): boolean {
  return trustedApp() !== null
}
