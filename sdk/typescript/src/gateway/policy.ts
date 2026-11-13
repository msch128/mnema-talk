// What a close code means for the next connection, and how long to wait.
import { GatewayCloseCode as C } from '../constants.ts'

/**
 * - `fatal`: stop and report (bad token, intents not allowed).
 * - `identify`: reconnect with a new session.
 * - `resume`: reconnect and resume the session.
 * - `rateLimited`: resume after a longer pause.
 */
export type CloseAction = 'fatal' | 'identify' | 'resume' | 'rateLimited'

/** Close code the SDK uses itself when the server stopped acknowledging heartbeats. */
export const ZOMBIE_CLOSE_CODE = 4900

const FATAL = new Set<number>([C.AuthenticationFailed, C.InvalidIntents, C.DisallowedIntents])
const NEW_SESSION = new Set<number>([C.SessionTakenOver, C.NotAuthenticated, C.AlreadyAuthenticated, C.InvalidSeq])
/** Close codes that point at a bug in the bot or the SDK; reported, not fatal. */
const PROTOCOL = new Set<number>([C.UnknownOpcode, C.DecodeError, C.AlreadyAuthenticated])

export function closeAction(code: number): CloseAction {
  if (FATAL.has(code)) return 'fatal'
  if (code === C.RateLimited) return 'rateLimited'
  if (NEW_SESSION.has(code)) return 'identify'
  return 'resume'
}

export function isProtocolError(code: number): boolean {
  return PROTOCOL.has(code)
}

const MEANINGS: Record<number, string> = {
  [C.SessionTakenOver]: 'session taken over by another connection',
  [C.UnknownOpcode]: 'unknown opcode',
  [C.DecodeError]: 'undecodable frame',
  [C.NotAuthenticated]: 'not authenticated in time',
  [C.AuthenticationFailed]: 'authentication failed: bad, revoked or expired token, or the bot is disabled, deleted or banned',
  [C.AlreadyAuthenticated]: 'already identified',
  [C.InvalidSeq]: 'invalid sequence number',
  [C.RateLimited]: 'rate limited: too many frames, IDENTIFYs or connections',
  [C.SessionTimedOut]: 'session timed out (missed heartbeats)',
  [C.InvalidIntents]: 'invalid intents',
  [C.DisallowedIntents]: 'intents the admin has not allowed for this bot',
  [ZOMBIE_CLOSE_CODE]: 'no heartbeat ACK from the server',
  1009: 'frame too large',
  1013: 'the bot reads too slowly',
}

export function closeMeaning(code: number): string {
  return MEANINGS[code] ?? 'connection closed'
}

export interface BackoffOptions {
  /** First delay, default 1000 ms. */
  minDelayMs?: number
  /** Longest delay, default 60000 ms. */
  maxDelayMs?: number
  /** Least delay after close code 4008, default 30000 ms. */
  rateLimitedDelayMs?: number
}

/** Exponential backoff with jitter: attempt n waits between half and all of min * 2^n, capped at max. */
export function backoffDelay(attempt: number, o: Required<BackoffOptions>, random: () => number = Math.random): number {
  const base = Math.min(o.maxDelayMs, o.minDelayMs * 2 ** attempt)
  return Math.round(base / 2 + (random() * base) / 2)
}

/** A connection the gateway closed with a fatal code. */
export class GatewayError extends Error {
  override name = 'GatewayError'
  readonly code: number
  readonly reason: string

  constructor(code: number, reason: string) {
    super(`gateway closed with ${code}: ${reason || closeMeaning(code)}`)
    this.code = code
    this.reason = reason
  }
}
