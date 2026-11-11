// What limits the composer, shown before it refuses: the longest message
// (internal/chat MaxMessageLen) and the wait of a channel's slow mode.

export const MAX_MESSAGE_LENGTH = 4000
/** The counter appears from here on; before that it would only be noise. */
export const COUNTER_FROM = 3600

export interface LengthState {
  /** Characters left; negative when the message is too long. */
  left: number
  over: boolean
}

/** The counter of a message, or null while it is far from the limit. The server counts characters, not UTF-16 units. */
export function lengthState(text: string): LengthState | null {
  // Characters never outnumber UTF-16 units, so a short text needs no counting.
  if (text.length < COUNTER_FROM) return null
  const count = Array.from(text.trim()).length
  if (count < COUNTER_FROM) return null
  return { left: MAX_MESSAGE_LENGTH - count, over: count > MAX_MESSAGE_LENGTH }
}

/** Milliseconds until a member may write again; 0 when they may. */
export function slowModeRemaining(readyAt: number | undefined, now: number): number {
  return readyAt === undefined ? 0 : Math.max(0, readyAt - now)
}
