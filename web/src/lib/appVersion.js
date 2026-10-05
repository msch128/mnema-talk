// The version this page was built as (vite define, same source as the Go
// binary's internal/version) and the rule for when to offer a reload.

export const CLIENT_VERSION = typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : 'dev'

const SAFE = /^[0-9A-Za-z.+_-]{1,64}$/

/** Normalizes a version string from the server; anything odd becomes ''. */
export function normalizeVersion(v) {
  if (typeof v !== 'string') return ''
  const s = v.trim().replace(/^v/, '')
  return SAFE.test(s) ? s : ''
}

/**
 * True when the server runs a different build than this page: both versions
 * are known, neither is a development build ("dev"), and they differ. The UI
 * only offers a reload; it never reloads by itself.
 */
export function isNewServerVersion(serverVersion, clientVersion = CLIENT_VERSION) {
  const server = normalizeVersion(serverVersion)
  const client = normalizeVersion(clientVersion)
  if (!server || !client || server === 'dev' || client === 'dev') return false
  return server !== client
}
