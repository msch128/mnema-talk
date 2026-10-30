// The calls of direct messages ("Start a Talk"): who is ringing me, whom I am
// calling, and which conversations have a call going on. The server decides
// (dm_call_ring and dm_call_end, and `call` in GET /api/dms); this keeps what
// the screen shows. The rules about whom to ring live in stores/chat/dmCalls.
import { defineStore } from 'pinia'
import { computed } from 'vue'
import type { ChatDMCall } from '../types/rest'
import type { DmCallEnd } from '../types/dmEvents'
import * as calls from './dmCalls/actions'
import type { CallInfo, CallMark } from './dmCalls/types'

export type { CallInfo, CallMark, IncomingCall, OutgoingCall, OutgoingPhase } from './dmCalls/types'

export const useDmCallsStore = defineStore('dmCalls', () => {
  const cs = calls.createCallState()
  return {
    incoming: cs.incoming,
    outgoing: cs.outgoing,
    active: cs.active,
    anyRinging: computed(() => cs.incoming.value.length > 0),
    anyCall: computed(() => cs.incoming.value.length > 0 || cs.outgoing.value !== null || Object.keys(cs.active.value).length > 0),
    ringIncoming: (call: CallInfo, seconds: number, silent: boolean) => calls.ringIncoming(cs, call, seconds, silent),
    ringOutgoing: (call: CallInfo, seconds: number) => calls.ringOutgoing(cs, call, seconds),
    end: (p: DmCallEnd) => calls.endCall(cs, p),
    dismiss: (conversationId: string) => calls.dismiss(cs, conversationId),
    joined: (talkId: string) => calls.joined(cs, talkId),
    clearOutgoing: () => calls.clearOutgoing(cs),
    syncList: (list: readonly { id: string; call?: ChatDMCall }[], meId: string | undefined, ring: (call: CallInfo, seconds: number, mine: boolean) => void) => calls.syncList(cs, list, meId, ring),
    markOf: (conversationId: string): CallMark | null => calls.markOf(cs, conversationId),
    track: (call: CallInfo) => calls.track(cs, call),
    untrack: (conversationId: string) => calls.untrack(cs, conversationId),
    reset: () => calls.resetCalls(cs)
  }
})
