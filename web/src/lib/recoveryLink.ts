// Links from account e-mails: /verify-email#token=… and /reset-password#token=….
// The token sits in the URL fragment, which the browser never sends to the
// server and never puts in a Referer header; the app reads it once and removes
// it from the address bar and the history entry.
export type RecoveryKind = 'verify' | 'reset'

export interface RecoveryLink {
  kind: RecoveryKind
  /** The token from the link; empty when the link carries none (or a malformed one). */
  token: string
}

const PATHS: Readonly<Record<string, RecoveryKind>> = { '/verify-email': 'verify', '/reset-password': 'reset' }
// 32 random bytes in URL-safe base64 without padding, as the server issues them.
const TOKEN = /^[A-Za-z0-9_-]{43}$/

/** The recovery link a page address stands for, or null for any other address. */
export function recoveryFromLocation(pathname: string, hash: string): RecoveryLink | null {
  const kind = PATHS[pathname.replace(/\/+$/, '')]
  if (!kind) return null
  const token = new URLSearchParams(hash.replace(/^#/, '')).get('token') ?? ''
  return { kind, token: TOKEN.test(token) ? token : '' }
}

/** Drops the token from the address bar and the current history entry. */
export function scrubRecoveryUrl(pathname: string): void {
  window.history.replaceState(null, '', pathname)
}
