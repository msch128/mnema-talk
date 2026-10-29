// The browser client retains same-origin cookies. A separately constructed client
// can inject its own transport without sharing unauthorized listeners or credentials.
// Response JSON stays unknown until an explicit runtime decoder accepts it.
import { t } from '../i18n'
import { formatDetailMessage } from './errorDetails'
import { isRecord, type Decoder } from '../types/validation'

const CODES = new Set([
  'INVALID_CREDENTIALS', 'UNAUTHORIZED', 'FORBIDDEN', 'NOT_FOUND', 'CONFLICT', 'PAYLOAD_TOO_LARGE',
  'UNSUPPORTED_MEDIA_TYPE', 'RATE_LIMITED', 'RULES_NOT_ACCEPTED', 'UNAVAILABLE', 'INTERNAL_ERROR'
])

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

// Codes whose server message is stock wording: the generic text says it all.
const PLAIN_CODES = new Set([
  'INVALID_CREDENTIALS', 'UNAUTHORIZED', 'NOT_FOUND', 'PAYLOAD_TOO_LARGE', 'UNSUPPORTED_MEDIA_TYPE',
  'RATE_LIMITED', 'RULES_NOT_ACCEPTED', 'INTERNAL_ERROR'
])

/** Unknown server sentence: the generic text of the code, followed by the
 * server's own words (English) so the reason is never lost. */
function formatErrorCode(code: unknown, message: string): string {
  if (code === 'INVALID_INPUT' && message) return message
  if (typeof code === 'string' && CODES.has(code)) {
    const generic = t(`errors.code.${code}`)
    return message && !PLAIN_CODES.has(code) ? `${generic} (${message})` : generic
  }
  return message || t('errors.unknown')
}

// Untrusted error bodies may be null, arrays or objects. Bound copy before
// attempting patterns or displaying it; no raw payload gets logged.
export function errorMessage(code: unknown, serverMessage?: unknown): string {
  const message = typeof serverMessage === 'string' ? serverMessage.slice(0, 1024) : ''
  if (message) {
    const formatted = formatDetailMessage(message)
    if (formatted) return formatted
  }
  return formatErrorCode(code, message)
}

export type UnauthorizedHandler = (error: ApiError) => void

/** Requests expose no direct transport headers, cookie or credential options.
 * Path and payload remain caller input; a transport must independently
 * enforce its fixed endpoint registry, body schemas and broker capabilities. */
export interface ApiRequest {
  readonly method: string
  readonly json?: unknown
  readonly form?: FormData
  readonly signal?: AbortSignal
}

export interface ApiReply {
  readonly status: number
  readonly body: unknown
}

export type ApiTransport = (path: string, request: ApiRequest) => Promise<ApiReply>

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

export interface ApiCall {
  <T>(path: string, options: DecodedApiOptions<T>): Promise<T>
  (path: string, options?: ApiOptions): Promise<unknown>
}

// Distinguish local browser serialization failures from connectivity errors.
class BrowserPreparationFailure {
  constructor(readonly cause: unknown) {}
}

function isValidReplyStatus(status: unknown): boolean {
  if (!Number.isInteger(status)) return false
  const s = status as number
  return s >= 100 && s <= 599
}

function isValidApiReply(response: unknown): response is ApiReply {
  return isRecord(response) && isValidReplyStatus(response['status'])
}

/** Sends one request; anything but a well-formed reply is a network error (an abort passes through). */
async function sendRequest(transport: ApiTransport, path: string, operation: ApiRequest): Promise<ApiReply> {
  let response: ApiReply
  try {
    response = await transport(path, operation)
  } catch (error: unknown) {
    if (error instanceof BrowserPreparationFailure) throw error.cause
    if (isRecord(error) && error['name'] === 'AbortError') throw error
    throw new ApiError(0, 'NETWORK', t('errors.network'))
  }
  if (!isValidApiReply(response)) {
    throw new ApiError(0, 'NETWORK', t('errors.network'))
  }
  return response
}

/** The error a non-2xx reply stands for, from the server's {error: {code, message}} body. */
function serverFailure(status: number, body: unknown) {
  const serverError = isRecord(body) && isRecord(body['error']) ? body['error'] : null
  const code = typeof serverError?.['code'] === 'string' && serverError['code'].length <= 64
    ? serverError['code'] : 'INTERNAL_ERROR'
  return new ApiError(status, code, errorMessage(code, serverError?.['message']))
}

export interface ApiClient {
  readonly api: ApiCall
  readonly onUnauthorized: (handler: UnauthorizedHandler) => () => boolean
  /** Fires when a write is refused because the community rules are not accepted. */
  readonly onRulesRequired: (handler: () => void) => () => boolean
}

function buildApiRequest(options: ApiOptions): ApiRequest {
  const { method = 'GET', json, form, signal } = options
  return {
    method,
    ...(json !== undefined ? { json } : form ? { form } : {}),
    ...(signal !== undefined ? { signal } : {})
  }
}

function handleUnauthorizedNotification(
  path: string,
  status: number,
  error: ApiError,
  shouldNotify: (() => boolean) | undefined,
  currentHandlers: Set<{ readonly handler: UnauthorizedHandler }>,
  targetHandlers: { readonly handler: UnauthorizedHandler }[]
) {
  if (status !== 401 || path === '/api/auth/login') return
  if (shouldNotify && !shouldNotify()) return
  for (const subscription of targetHandlers) {
    if (currentHandlers.has(subscription)) {
      subscription.handler(error)
    }
  }
}

/** Construct once per session owner. Transport is immutable and every
 * client owns its listeners; choosing a profile never replaces global fetch. */
export function createApiClient(transport: ApiTransport): ApiClient {
  const unauthorizedHandlers = new Set<{ readonly handler: UnauthorizedHandler }>()
  const rulesHandlers = new Set<{ readonly handler: () => void }>()
  async function request<T>(path: string, options: ApiOptions & { decode?: Decoder<T> } = {}): Promise<unknown> {
    const { decode, shouldNotifyUnauthorized } = options
    const requestUnauthorizedHandlers = [...unauthorizedHandlers]
    const operation = buildApiRequest(options)
    const response = await sendRequest(transport, path, operation)
    if (response.status === 204) return decode ? decode(null) : null
    const body = response.body
    if (response.status < 200 || response.status >= 300) {
      const error = serverFailure(response.status, body)
      handleUnauthorizedNotification(path, response.status, error, shouldNotifyUnauthorized, unauthorizedHandlers, requestUnauthorizedHandlers)
      if (error.code === 'RULES_NOT_ACCEPTED') for (const { handler } of [...rulesHandlers]) handler()
      throw error
    }
    return decode ? decode(body) : body
  }
  return Object.freeze({
    api: request as ApiCall,
    onUnauthorized: (handler: UnauthorizedHandler) => {
      const subscription = { handler }
      unauthorizedHandlers.add(subscription)
      return () => unauthorizedHandlers.delete(subscription)
    },
    onRulesRequired: (handler: () => void) => {
      const subscription = { handler }
      rulesHandlers.add(subscription)
      return () => rulesHandlers.delete(subscription)
    }
  })
}

const browserTransport: ApiTransport = async (path, { method, json, form, signal }) => {
  const headers: Record<string, string> = {}
  const init: RequestInit = { method, credentials: 'same-origin', headers }
  if (signal !== undefined) init.signal = signal
  if (json !== undefined) {
    headers['Content-Type'] = 'application/json'
    try {
      init.body = JSON.stringify(json)
    } catch (error: unknown) {
      throw new BrowserPreparationFailure(error)
    }
  } else if (form) {
    init.body = form
  }
  const response = await fetch(path, init)
  return { status: response.status, body: response.status === 204 ? null : await response.json().catch(() => null) }
}

const browserClient = createApiClient(browserTransport)
export const api: ApiCall = browserClient.api
export const onUnauthorized = browserClient.onUnauthorized
export const onRulesRequired = browserClient.onRulesRequired
