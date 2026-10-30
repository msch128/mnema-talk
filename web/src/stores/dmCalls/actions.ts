// What happens to the calls of direct messages, as plain functions over the
// state the store keeps (stores/dmCalls). The rules about whom to ring live in
// stores/chat/dmCalls.
import { ref, type Ref } from 'vue'
import type { ChatDMCall } from '../../types/rest'
import type { DmCallEnd } from '../../types/dmEvents'
import type { CallInfo, CallMark, IncomingCall, OutgoingCall } from './types'

export interface CallState {
  incoming: Ref<IncomingCall[]>
  outgoing: Ref<OutgoingCall | null>
  active: Ref<Record<string, CallInfo>>
  /** Talks whose incoming call the member closed: it does not come back by itself. */
  dismissed: Set<string>
  timers: Map<string, ReturnType<typeof setTimeout>>
}

export function createCallState(): CallState {
  return { incoming: ref([]), outgoing: ref(null), active: ref({}), dismissed: new Set(), timers: new Map() }
}

function stopTimer(cs: CallState, conversationId: string) {
  clearTimeout(cs.timers.get(conversationId))
  cs.timers.delete(conversationId)
}

function startTimer(cs: CallState, conversationId: string, ms: number, then: () => void) {
  stopTimer(cs, conversationId)
  cs.timers.set(conversationId, setTimeout(() => {
    cs.timers.delete(conversationId)
    then()
  }, ms))
}

export function track(cs: CallState, call: CallInfo) {
  cs.active.value = { ...cs.active.value, [call.conversationId]: call }
}

export function untrack(cs: CallState, conversationId: string) {
  if (!(conversationId in cs.active.value)) return
  const rest = { ...cs.active.value }
  delete rest[conversationId]
  cs.active.value = rest
}

function removeIncoming(cs: CallState, conversationId: string) {
  cs.incoming.value = cs.incoming.value.filter(c => c.conversationId !== conversationId)
}

/** A call rings for me; it stops by itself after `seconds`. */
export function ringIncoming(cs: CallState, call: CallInfo, seconds: number, silent: boolean) {
  track(cs, call)
  if (cs.dismissed.has(call.talkId)) return
  const ms = seconds * 1000
  cs.incoming.value = [{ ...call, until: Date.now() + ms, silent }, ...cs.incoming.value.filter(c => c.conversationId !== call.conversationId)]
  startTimer(cs, call.conversationId, ms, () => removeIncoming(cs, call.conversationId))
}

function nobodyJoined(cs: CallState, talkId: string) {
  if (cs.outgoing.value?.talkId === talkId) cs.outgoing.value = { ...cs.outgoing.value, phase: 'nobody' }
}

/** I started a call: it rings for the others for `seconds`, then nobody has joined. */
export function ringOutgoing(cs: CallState, call: CallInfo, seconds: number) {
  track(cs, call)
  const ms = seconds * 1000
  cs.outgoing.value = { ...call, phase: 'calling', until: Date.now() + ms }
  startTimer(cs, call.conversationId, ms, () => nobodyJoined(cs, call.talkId))
}

/** The call stopped ringing or is over, as the server says. */
export function endCall(cs: CallState, p: DmCallEnd) {
  const mine = cs.outgoing.value?.talkId === p.talk_id
  const ringing = cs.incoming.value.some(c => c.conversationId === p.conversation_id && c.talkId === p.talk_id)
  if (ringing) removeIncoming(cs, p.conversation_id)
  if (mine || ringing) stopTimer(cs, p.conversation_id)
  if (mine) {
    if (p.reason === 'timeout' && p.active) nobodyJoined(cs, p.talk_id)
    else cs.outgoing.value = null
  }
  if (!p.active && cs.active.value[p.conversation_id]?.talkId === p.talk_id) untrack(cs, p.conversation_id)
  if (!p.active) cs.dismissed.delete(p.talk_id)
}

/** The member closed the incoming call; the call itself goes on. */
export function dismiss(cs: CallState, conversationId: string) {
  const call = cs.incoming.value.find(c => c.conversationId === conversationId)
  if (!call) return
  cs.dismissed.add(call.talkId)
  removeIncoming(cs, conversationId)
  stopTimer(cs, conversationId)
}

/** The member joined the Talk here: nothing rings for it any more. */
export function joined(cs: CallState, talkId: string) {
  const call = cs.incoming.value.find(c => c.talkId === talkId)
  if (!call) return
  removeIncoming(cs, call.conversationId)
  stopTimer(cs, call.conversationId)
}

/** The caller hung up or gave up on their own call. */
export function clearOutgoing(cs: CallState) {
  if (!cs.outgoing.value) return
  stopTimer(cs, cs.outgoing.value.conversationId)
  cs.outgoing.value = null
}

function infoOf(conversationId: string, call: ChatDMCall): CallInfo {
  return { conversationId, talkId: call.talk_id, messageId: call.message_id, callerId: call.caller_id, talkName: call.talk_name }
}

/**
 * The calls the server lists with the conversations replace what is held (a
 * reconnect, a reload). `ring` is told of a call that rings and is not shown yet.
 */
export function syncList(cs: CallState, list: readonly { id: string; call?: ChatDMCall }[], meId: string | undefined, ring: (call: CallInfo, seconds: number, mine: boolean) => void) {
  const listed = new Set(list.filter(dm => dm.call).map(dm => dm.id))
  for (const id of Object.keys(cs.active.value)) if (!listed.has(id)) untrack(cs, id)
  cs.incoming.value = cs.incoming.value.filter(c => listed.has(c.conversationId))
  if (cs.outgoing.value && !listed.has(cs.outgoing.value.conversationId)) clearOutgoing(cs)
  for (const dm of list) {
    if (!dm.call) continue
    const info = infoOf(dm.id, dm.call)
    const shown = cs.incoming.value.some(c => c.talkId === info.talkId) || cs.outgoing.value?.talkId === info.talkId
    if (dm.call.ringing && !shown) ring(info, dm.call.ring_seconds, dm.call.caller_id === meId)
    else track(cs, info)
  }
}

export function markOf(cs: CallState, conversationId: string): CallMark | null {
  if (cs.incoming.value.some(c => c.conversationId === conversationId)) return 'ringing'
  if (cs.outgoing.value?.conversationId === conversationId && cs.outgoing.value.phase === 'calling') return 'calling'
  return conversationId in cs.active.value ? 'active' : null
}

export function resetCalls(cs: CallState) {
  for (const id of [...cs.timers.keys()]) stopTimer(cs, id)
  cs.incoming.value = []
  cs.outgoing.value = null
  cs.active.value = {}
  cs.dismissed.clear()
}
