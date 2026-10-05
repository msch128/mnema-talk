// Single entry point for HTTP calls to the backend. The session lives in an
// HttpOnly cookie, so no token is ever handled in JavaScript; same-origin
// requests carry the cookie automatically.

import { t } from '../i18n'

// Server error codes → keys in the locale files (errors.code.*).
const CODES = [
  'INVALID_CREDENTIALS', 'UNAUTHORIZED', 'FORBIDDEN', 'NOT_FOUND', 'CONFLICT', 'PAYLOAD_TOO_LARGE',
  'UNSUPPORTED_MEDIA_TYPE', 'RATE_LIMITED', 'UNAVAILABLE', 'INTERNAL_ERROR'
]

// Specific validation messages from the server → keys (errors.detail.*).
const DETAILS = {
  'invalid invite code': 'invalidInvite',
  'invite code has expired': 'inviteExpired',
  'invite code usage limit reached': 'inviteUsedUp',
  'username is already taken': 'usernameTaken',
  'current password is incorrect': 'wrongPassword'
}

export class ApiError extends Error {
  constructor(status, code, message) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

const PATTERNS = [
  [/^password must be at least (\d+) characters$/, n => t('errors.detail.passwordMin', { count: n })],
  [/^password must be at most/, () => t('errors.detail.passwordMax')],
  [/^username must be/, () => t('errors.detail.usernameFormat')],
  [/^(\w+) must be at most (\d+) characters$/, (field, n) => t('errors.detail.fieldMax', { field: FIELDS[field] ? t(`errors.field.${field}`) : field, count: n })],
  [/^file exceeds the (\d+) MB limit$/, n => t('errors.detail.fileTooBig', { size: n })]
]

const FIELDS = { content: 1, bio: 1, display_name: 1, name: 1, topic: 1 }

export function errorMessage(code, serverMessage) {
  if (serverMessage && DETAILS[serverMessage]) return t(`errors.detail.${DETAILS[serverMessage]}`)
  for (const [re, fmt] of PATTERNS) {
    const m = serverMessage?.match(re)
    if (m) return fmt(...m.slice(1))
  }
  if (code === 'INVALID_INPUT' && serverMessage) return serverMessage
  if (CODES.includes(code)) return t(`errors.code.${code}`)
  return serverMessage || t('errors.unknown')
}

const unauthorizedHandlers = new Set()

/** Registers a callback for 401 responses (session expired or revoked). */
export function onUnauthorized(fn) {
  unauthorizedHandlers.add(fn)
  return () => unauthorizedHandlers.delete(fn)
}

/**
 * api('/api/channels') → parsed JSON (or null for 204).
 * Options: method, json (object body), form (FormData), signal.
 */
export async function api(path, { method = 'GET', json, form, signal } = {}) {
  const init = { method, credentials: 'same-origin', headers: {}, signal }
  if (json !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(json)
  } else if (form) {
    init.body = form
  }

  let res
  try {
    res = await fetch(path, init)
  } catch (err) {
    if (err?.name === 'AbortError') throw err
    throw new ApiError(0, 'NETWORK', t('errors.network'))
  }

  if (res.status === 204) return null
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    const code = body?.error?.code || 'INTERNAL_ERROR'
    const err = new ApiError(res.status, code, errorMessage(code, body?.error?.message))
    if (res.status === 401 && path !== '/api/auth/login') {
      unauthorizedHandlers.forEach(fn => fn(err))
    }
    throw err
  }
  return body
}
