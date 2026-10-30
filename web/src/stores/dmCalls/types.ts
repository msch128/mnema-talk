export interface CallInfo {
  conversationId: string
  talkId: string
  /** The invitation card in the conversation. */
  messageId: string
  callerId: string
  talkName: string
}

/** A call ringing for me. `until` is when it stops (client clock). */
export interface IncomingCall extends CallInfo {
  until: number
  /** No sound: the member is in do-not-disturb, or already in a call. */
  silent: boolean
}

export type OutgoingPhase = 'calling' | 'nobody'

/** The call I started: ringing for the others, or nobody joined in time. */
export interface OutgoingCall extends CallInfo {
  phase: OutgoingPhase
  until: number
}

/** How a conversation's row is marked: ringing for me, me calling, or a call going on. */
export type CallMark = 'ringing' | 'calling' | 'active'
