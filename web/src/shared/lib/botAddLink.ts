// The add link of a bot (doc/bots.md, Adding a bot from a manifest):
// /admin/bots/add#manifest=<base64url>. The manifest travels in the fragment,
// which the app's own address handling drops (the console rewrites its address,
// signing in remembers only the path). So it is taken out of the address before
// the app starts, kept for this tab until the Bots tab picks it up, and the
// address becomes the Bots tab's: the manifest stays out of the history too.
const KEY = 'mnema_pending_bot_manifest'
const ADD_PATH = /^\/admin\/bots\/add\/?$/
// A manifest with an avatar of 256 KB is about 350,000 characters of base64url.
const MAX_PARAM = 400_000

/** The base64url manifest of an add link's fragment, or null. */
export function manifestParam(hash: string): string | null {
  const value = new URLSearchParams(hash.replace(/^#/, '')).get('manifest')
  return value && value.length <= MAX_PARAM && /^[A-Za-z0-9_-]+={0,2}$/.test(value) ? value : null
}

/** Whether the address is an add link. */
export function isBotAddPath(pathname: string): boolean {
  return ADD_PATH.test(pathname)
}

/**
 * Moves the manifest of an add link into this tab's storage and leaves the
 * Bots tab's address. True when the address was an add link.
 */
export function captureBotAddLink(location: Pick<Location, 'pathname' | 'hash'> = window.location): boolean {
  if (!isBotAddPath(location.pathname)) return false
  const param = manifestParam(location.hash)
  try {
    if (param) sessionStorage.setItem(KEY, param)
  } catch { /* storage is blocked: the admin pastes the manifest instead */ }
  try {
    window.history.replaceState(window.history.state, '', '/admin/bots')
  } catch { /* the address stays as it is */ }
  return true
}

/** Whether an add link is waiting for the Bots tab. */
export function hasPendingBotManifest(): boolean {
  try {
    return sessionStorage.getItem(KEY) !== null
  } catch {
    return false
  }
}

/** The waiting add link's manifest (base64url), removed from storage. */
export function takePendingBotManifest(): string | null {
  try {
    const value = sessionStorage.getItem(KEY)
    sessionStorage.removeItem(KEY)
    return value
  } catch {
    return null
  }
}

/** Drops a waiting add link (the member may not add bots). */
export function dropPendingBotManifest(): void {
  try {
    sessionStorage.removeItem(KEY)
  } catch { /* nothing kept */ }
}
