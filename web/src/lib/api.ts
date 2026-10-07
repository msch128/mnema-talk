// Same-origin cookies remain the sole HTTP session transport. Response JSON
// stays unknown until an explicit runtime decoder establishes its contract.
import { t } from '../i18n'
import { isRecord, type Decoder } from '../types/validation'

const CODES = new Set([
  'INVALID_CREDENTIALS', 'UNAUTHORIZED', 'FORBIDDEN', 'NOT_FOUND', 'CONFLICT', 'PAYLOAD_TOO_LARGE',
  'UNSUPPORTED_MEDIA_TYPE', 'RATE_LIMITED', 'UNAVAILABLE', 'INTERNAL_ERROR'
])

const DETAILS: Readonly<Record<string, string>> = {
  'invalid invite code': 'invalidInvite',
  'invite code has expired': 'inviteExpired',
  'invite code usage limit reached': 'inviteUsedUp',
  'username is already taken': 'usernameTaken',
  'current password is incorrect': 'wrongPassword'
}

export class ApiError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

/** Preserve ordinary Error/plain {message} failures without assuming the
 * thrown value is an Error. The fallback belongs to the user's action. */
export function caughtErrorMessage(error: unknown, fallback: string): string {
  return isRecord(error) && typeof error['message'] === 'string' && error['message']
    ? error['message'].slice(0, 1024) : fallback
}

type ErrorPattern = readonly [RegExp, (...matches: string[]) => string]
const FIELDS = new Set(['content', 'bio', 'display_name', 'name', 'topic'])
const PATTERNS: readonly ErrorPattern[] = [
  [/^password must be at least (\d+) characters$/, n => t('errors.detail.passwordMin', { count: n })],
  [/^password must be at most/, () => t('errors.detail.passwordMax')],
  [/^username must be/, () => t('errors.detail.usernameFormat')],
  [/^(\w+) must be at most (\d+) characters$/, (field = '', n = '') => t('errors.detail.fieldMax', { field: FIELDS.has(field) ? t(`errors.field.${field}`) : field, count: n })],
  [/^file exceeds the (\d+) MB limit$/, n => t('errors.detail.fileTooBig', { size: n })]
]

// Untrusted error bodies may be null, arrays or objects. Bound copy before
// attempting patterns or displaying it; no raw payload gets logged.
export function errorMessage(code: unknown, serverMessage?: unknown): string {
  const message = typeof serverMessage === 'string' ? serverMessage.slice(0, 1024) : ''
  if (message && Object.prototype.hasOwnProperty.call(DETAILS, message)) return t(`errors.detail.${DETAILS[message]}`)
  for (const [pattern, format] of PATTERNS) {
    const match = message.match(pattern)
    if (match) return format(...match.slice(1))
  }
  if (code === 'INVALID_INPUT' && message) return message
  if (typeof code === 'string' && CODES.has(code)) return t(`errors.code.${code}`)
  return message || t('errors.unknown')
}

type UnauthorizedHandler = (error: ApiError) => void
const unauthorizedHandlers = new Set<UnauthorizedHandler>()

export function onUnauthorized(handler: UnauthorizedHandler): () => boolean {
  unauthorizedHandlers.add(handler)
  return () => unauthorizedHandlers.delete(handler)
}

export interface ApiOptions {
  method?: string
  json?: unknown
  form?: FormData
  signal?: AbortSignal
  /** A queued session request may become stale before its HTTP response. */
  shouldNotifyUnauthorized?: () => boolean
}

export interface DecodedApiOptions<T> extends ApiOptions {
  decode: Decoder<T>
}

export function api<T>(path: string, options: DecodedApiOptions<T>): Promise<T>
export function api(path: string, options?: ApiOptions): Promise<unknown>
export async function api<T>(path: string, options: ApiOptions & { decode?: Decoder<T> } = {}): Promise<unknown> {
  const { method = 'GET', json, form, signal, decode, shouldNotifyUnauthorized } = options
  const requestUnauthorizedHandlers = [...unauthorizedHandlers]
  const headers: Record<string, string> = {}
  const init: RequestInit = { method, credentials: 'same-origin', headers }
  if (signal !== undefined) init.signal = signal
  if (json !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(json)
  } else if (form) {
    init.body = form
  }

  let response: Response
  try {
    response = await fetch(path, init)
  } catch (error: unknown) {
    if (isRecord(error) && error['name'] === 'AbortError') throw error
    throw new ApiError(0, 'NETWORK', t('errors.network'))
  }

  if (response.status === 204) return decode ? decode(null) : null
  const body: unknown = await response.json().catch(() => null)
  if (!response.ok) {
    const serverError = isRecord(body) && isRecord(body['error']) ? body['error'] : null
    const code = typeof serverError?.['code'] === 'string' && serverError['code'].length <= 64
      ? serverError['code'] : 'INTERNAL_ERROR'
    const error = new ApiError(response.status, code, errorMessage(code, serverError?.['message']))
    if (response.status === 401 && path !== '/api/auth/login' && (shouldNotifyUnauthorized?.() ?? true)) {
      requestUnauthorizedHandlers.forEach(handler => {
        if (unauthorizedHandlers.has(handler)) handler(error)
      })
    }
    throw error
  }
  return decode ? decode(body) : body
}
