// The request path of the REST client: buckets, retries after 429, timeouts
// and the mapping of error answers to RestError.
import { API_BASE_PATH } from '../constants.ts'
import { sleep, trimSlash, type Logger } from '../util.ts'
import { VERSION } from '../version.ts'
import { RestError } from './error.ts'
import { Bucket, WindowLimiter, bucketKey, isMessagePost } from './limits.ts'

export interface RestOptions {
  /** The server, for example https://talk.example.org (a trailing slash is fine). */
  baseUrl: string
  /** The bot token (mnb_...). */
  token: string
  /** Per attempt, default 15000 ms. */
  timeoutMs?: number
  /** Retries after a 429 (and after 502/503/504 on GET), default 3. */
  maxRetries?: number
  /**
   * Hold requests back before the server would refuse them (50 per second,
   * 5 messages per 5 seconds per channel). Default true; 429s are honoured
   * either way.
   */
  localRateLimits?: boolean
  /** Told about every 429 the server answered. */
  onRateLimit?: (info: RateLimitInfo) => void
  logger?: Logger
  /** A fetch implementation, default the global fetch. */
  fetch?: typeof fetch
  userAgent?: string
}

export interface RateLimitInfo {
  method: string
  path: string
  bucket: string
  /** Seconds the server asked to wait. */
  retryAfter: number
  global: boolean
  /** The X-RateLimit-Scope header, when sent. */
  scope: string | undefined
  /** 1 for the first 429 of this request. */
  attempt: number
  /** Whether the request is sent again after the wait. */
  willRetry: boolean
}

export interface RequestOptions {
  query?: Record<string, string | number | boolean | undefined>
  /** Sent as JSON. */
  body?: unknown
  /** Sent as multipart/form-data. */
  form?: FormData
  signal?: AbortSignal
}

const RETRYABLE_GET = new Set([502, 503, 504])

export class RestCore {
  readonly baseUrl: string
  private readonly token: string
  private readonly timeoutMs: number
  private readonly maxRetries: number
  private readonly local: boolean
  private readonly fetchImpl: typeof fetch
  private readonly userAgent: string
  private readonly onRateLimit: RestOptions['onRateLimit']
  private readonly logger: Logger | undefined
  private readonly buckets = new Map<string, Bucket>()
  private readonly globalLimit = new WindowLimiter(50, 1000)
  private globalPausedUntil = 0

  constructor(o: RestOptions) {
    if (!o.baseUrl) throw new TypeError('baseUrl is required')
    if (!o.token) throw new TypeError('token is required')
    this.baseUrl = trimSlash(o.baseUrl)
    this.token = o.token
    this.timeoutMs = o.timeoutMs ?? 15_000
    this.maxRetries = o.maxRetries ?? 3
    this.local = o.localRateLimits ?? true
    this.fetchImpl = o.fetch ?? globalThis.fetch.bind(globalThis)
    this.userAgent = o.userAgent ?? `mnema-talk-sdk/${VERSION}`
    this.onRateLimit = o.onRateLimit
    this.logger = o.logger
  }

  /** Sends a request to `${baseUrl}/api/bot/v1${path}` and answers the parsed body (undefined for 204). */
  async request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
    const key = bucketKey(method, path)
    const bucket = this.bucket(key)
    for (let attempt = 0; ; attempt++) {
      await this.acquire(bucket, opts.signal)
      const res = await this.send(method, path, opts)
      if (res.status === 429) {
        const wait = await this.noteRateLimit(res, bucket, { method, path, key, attempt, maxRetries: this.maxRetries })
        if (wait.willRetry) continue
        throw new RestError({ status: 429, method, path, body: wait.body, retryAfter: wait.retryAfter, global: wait.global })
      }
      if (method === 'GET' && RETRYABLE_GET.has(res.status) && attempt < this.maxRetries) {
        await res.body?.cancel()
        await sleep(500 * 2 ** attempt, opts.signal)
        continue
      }
      return (await this.parse(res, method, path)) as T
    }
  }

  private bucket(key: string): Bucket {
    let b = this.buckets.get(key)
    if (!b) {
      b = new Bucket(isMessagePost(key) ? new WindowLimiter(5, 5000) : undefined)
      this.buckets.set(key, b)
    }
    return b
  }

  /** Waits until the global limit, the route's bucket and any 429 pause allow a request. */
  private async acquire(bucket: Bucket, signal?: AbortSignal): Promise<void> {
    for (;;) {
      const now = Date.now()
      let wait = Math.max(this.globalPausedUntil, bucket.pausedUntil) - now
      if (this.local) {
        wait = Math.max(wait, this.globalLimit.delay(now), bucket.limiter?.delay(now) ?? 0)
      }
      if (wait <= 0) {
        if (this.local) {
          this.globalLimit.take(now)
          bucket.limiter?.take(now)
        }
        return
      }
      await sleep(wait, signal)
    }
  }

  private async send(method: string, path: string, opts: RequestOptions): Promise<Response> {
    const headers: Record<string, string> = {
      Authorization: `Bot ${this.token}`,
      'User-Agent': this.userAgent,
      Accept: 'application/json',
    }
    let body: BodyInit | undefined
    if (opts.form) body = opts.form
    else if (opts.body !== undefined) {
      headers['Content-Type'] = 'application/json'
      body = JSON.stringify(opts.body)
    }
    const timeout = AbortSignal.timeout(this.timeoutMs)
    const signal = opts.signal ? AbortSignal.any([opts.signal, timeout]) : timeout
    this.logger?.debug?.(`rest ${method} ${path}`)
    return this.fetchImpl(this.url(path, opts.query), { method, headers, body, signal })
  }

  private url(path: string, query: RequestOptions['query']): string {
    const u = new URL(this.baseUrl + API_BASE_PATH + path)
    for (const [k, v] of Object.entries(query ?? {})) {
      if (v !== undefined) u.searchParams.set(k, String(v))
    }
    return u.toString()
  }

  private async noteRateLimit(
    res: Response,
    bucket: Bucket,
    ctx: { method: string; path: string; key: string; attempt: number; maxRetries: number },
  ): Promise<{ retryAfter: number; global: boolean; body: unknown; willRetry: boolean }> {
    const body = await readBody(res)
    const b = (body ?? {}) as { retry_after?: unknown; global?: unknown }
    const retryAfter = retryAfterSeconds(b.retry_after, res.headers.get('retry-after'))
    const global = b.global === true || res.headers.get('x-ratelimit-global') === 'true'
    // A wait beyond the cap is not taken: the request fails with the 429 and
    // the bot decides, instead of every route stalling behind one answer.
    const withinCap = retryAfter <= MAX_RETRY_AFTER_S
    const willRetry = ctx.attempt < ctx.maxRetries && withinCap
    if (withinCap) {
      const until = Date.now() + Math.ceil(retryAfter * 1000) + Math.floor(Math.random() * RETRY_JITTER_MS)
      if (global) this.globalPausedUntil = Math.max(this.globalPausedUntil, until)
      else bucket.pausedUntil = Math.max(bucket.pausedUntil, until)
    }
    const info: RateLimitInfo = {
      method: ctx.method,
      path: ctx.path,
      bucket: ctx.key,
      retryAfter,
      global,
      scope: res.headers.get('x-ratelimit-scope') ?? undefined,
      attempt: ctx.attempt + 1,
      willRetry,
    }
    this.logger?.warn?.(`rate limited on ${ctx.method} ${ctx.path}: retry after ${retryAfter}s${global ? ' (global)' : ''}`)
    this.onRateLimit?.(info)
    return { retryAfter, global, body, willRetry }
  }

  private async parse(res: Response, method: string, path: string): Promise<unknown> {
    const body = await readBody(res)
    if (!res.ok) throw new RestError({ status: res.status, method, path, body })
    return body
  }
}

async function readBody(res: Response): Promise<unknown> {
  const text = await res.text()
  if (text === '') return undefined
  if ((res.headers.get('content-type') ?? '').includes('json')) {
    try {
      return JSON.parse(text)
    } catch {
      return text
    }
  }
  return text
}

/** The longest 429 wait the client takes on its own, in seconds. */
export const MAX_RETRY_AFTER_S = 60
// Spreads retries a little so that bots on one host do not return at once.
const RETRY_JITTER_MS = 250

/** The wait a 429 asks for: the body's retry_after, else Retry-After, else 1 s. */
function retryAfterSeconds(body: unknown, header: string | null): number {
  if (typeof body === 'number' && Number.isFinite(body) && body >= 0) return body
  const h = Number(header)
  return Number.isFinite(h) && h > 0 ? h : 1
}
