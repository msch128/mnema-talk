/**
 * An error answer of the REST API, in Discord's shape `{message, code}`:
 * `message` is the server's message, `status`, `method` and `path` say where.
 */
export class RestError extends Error {
  override name = 'RestError'
  /** HTTP status. */
  readonly status: number
  /** Discord's JSON error code: 10003 unknown channel, 10008 unknown message, 50013 missing permissions, 50035 invalid form body, 0 plain HTTP errors. */
  readonly code: number
  /** The parsed body (or the raw text when it was not JSON). */
  readonly body: unknown
  readonly method: string
  readonly path: string
  /** Seconds to wait, on a 429 that ran out of retries. */
  readonly retryAfter: number | undefined
  /** Whether a 429 hit the bot's overall limit. */
  readonly global: boolean | undefined

  constructor(init: {
    status: number
    method: string
    path: string
    body: unknown
    retryAfter?: number
    global?: boolean
  }) {
    const b = (typeof init.body === 'object' && init.body !== null ? init.body : {}) as { message?: unknown; code?: unknown }
    const message = typeof b.message === 'string' ? b.message : `HTTP ${init.status}`
    super(message)
    this.status = init.status
    this.code = typeof b.code === 'number' ? b.code : 0
    this.body = init.body
    this.method = init.method
    this.path = init.path
    this.retryAfter = init.retryAfter
    this.global = init.global
  }
}
