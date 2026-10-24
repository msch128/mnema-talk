// Stage: who is on stage and who listens (doc/talks.md). The server decides
// what may be sent; this only arranges what the interface shows.

/** What a Stage needs to know about a participant (the server's talk_power and hand_raised_at). */
export interface StageSeat {
  id: string
  talk_power?: boolean | undefined
  hand_raised_at?: string | undefined
}

export interface StageSplit<T extends StageSeat> {
  /** Members who may speak, in the order the Talk lists them. */
  speakers: T[]
  /** Everybody else. */
  audience: T[]
  /** Audience members with a raised hand, the one waiting longest first. */
  hands: T[]
}

export function splitStage<T extends StageSeat>(users: readonly T[]): StageSplit<T> {
  const speakers = users.filter(u => u.talk_power === true)
  const audience = users.filter(u => u.talk_power !== true)
  const hands = audience
    .flatMap(user => (user.hand_raised_at ? [{ user, at: Date.parse(user.hand_raised_at) }] : []))
    .sort((a, b) => a.at - b.at)
    .map(entry => entry.user)
  return { speakers, audience, hands }
}

/** The live reaction rate the server allows (internal/ws reactions): the button waits this out. */
export const REACTIONS_PER_WINDOW = 5
export const REACTION_WINDOW_MS = 10_000

/** A small client-side mirror of the server's allowance so a held-down button does not send what is dropped. */
export function createReactionLimiter(now: () => number = Date.now) {
  let sent: number[] = []
  return () => {
    const t = now()
    sent = sent.filter(at => t - at < REACTION_WINDOW_MS)
    if (sent.length >= REACTIONS_PER_WINDOW) return false
    sent.push(t)
    return true
  }
}
