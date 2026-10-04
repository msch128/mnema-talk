// Single entry point for HTTP calls to the backend. The session lives in an
// HttpOnly cookie, so no token is ever handled in JavaScript; same-origin
// requests carry the cookie automatically.

const MESSAGES = {
  INVALID_CREDENTIALS: 'Benutzername oder Passwort ist falsch',
  UNAUTHORIZED: 'Bitte melde dich erneut an',
  FORBIDDEN: 'Dafür fehlt dir die Berechtigung',
  NOT_FOUND: 'Nicht gefunden',
  CONFLICT: 'Das gibt es bereits',
  PAYLOAD_TOO_LARGE: 'Die Datei ist zu groß',
  UNSUPPORTED_MEDIA_TYPE: 'Dieser Dateityp wird nicht unterstützt',
  RATE_LIMITED: 'Zu viele Versuche – bitte warte einen Moment',
  UNAVAILABLE: 'Der Dienst ist gerade nicht verfügbar',
  INTERNAL_ERROR: 'Interner Serverfehler'
}

// Specific validation messages from the server, translated for the UI.
const DETAILS = {
  'invalid invite code': 'Ungültiger Einladungscode',
  'invite code has expired': 'Der Einladungscode ist abgelaufen',
  'invite code usage limit reached': 'Der Einladungscode wurde bereits zu oft verwendet',
  'username is already taken': 'Dieser Benutzername ist bereits vergeben',
  'current password is incorrect': 'Das aktuelle Passwort ist falsch'
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
  [/^password must be at least (\d+) characters$/, n => `Das Passwort muss mindestens ${n} Zeichen haben`],
  [/^password must be at most/, () => 'Das Passwort ist zu lang'],
  [/^username must be/, () => 'Benutzername: 3–32 Zeichen, nur Buchstaben, Ziffern, _ . oder -'],
  [/^(\w+) must be at most (\d+) characters$/, (field, n) => `${FIELDS[field] || field}: höchstens ${n} Zeichen`],
  [/^file exceeds the (\d+) MB limit$/, n => `Die Datei ist größer als ${n} MB`]
]

const FIELDS = { content: 'Nachricht', bio: 'Biografie', display_name: 'Anzeigename', name: 'Name', topic: 'Thema' }

export function errorMessage(code, serverMessage) {
  if (serverMessage && DETAILS[serverMessage]) return DETAILS[serverMessage]
  for (const [re, fmt] of PATTERNS) {
    const m = serverMessage?.match(re)
    if (m) return fmt(...m.slice(1))
  }
  if (code === 'INVALID_INPUT' && serverMessage) return serverMessage
  return MESSAGES[code] || serverMessage || 'Unbekannter Fehler'
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
    throw new ApiError(0, 'NETWORK', 'Keine Verbindung zum Server')
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
