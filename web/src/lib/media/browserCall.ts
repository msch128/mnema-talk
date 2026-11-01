import { mediaUserId } from './bounds'
import { createMediaGuard, type MediaGuard } from './guard'
import { mediaSealingSupported } from './support'
import { WirePacer } from './pacer'
import type { MediaCall } from './api'
import { work, newCallState, rosterIds, track, flush, startCall, stopCall, handleStats } from './callEngine'

export function mediaSettled(call: MediaCall): Promise<void> {
  return work.get(call) ?? Promise.resolve()
}

/** The call the browser actually runs. Tests install a double in front of it. */
export function browserMediaCall(guard: MediaGuard = createMediaGuard()): MediaCall {
  const s = newCallState(guard, new WirePacer())

  const call: MediaCall = {
    start(nextChannel, nextUser) { startCall(s, nextChannel, nextUser) },
    roster(ids) {
      s.roster = rosterIds(ids, s.userId)
      s.group?.hint(s.roster)
      track(s, flush(s, s.generation))
    },
    receive(fromUserId, wire) {
      s.inbox.push({ from: fromUserId, wire })
      track(s, flush(s, s.generation))
    },
    resume() {
      track(s, flush(s, s.generation, true))
    },
    stop() { stopCall(s) },
    onKey(listener) { s.keyListener = listener },
    onWire(listener) { s.wireListener = listener },
    onNotice(listener) { s.noticeListener = listener },
    onVerification(listener) { s.verificationListener = listener },
    protectSender(sender, source, layer, dtx) {
      if (!s.channelId || !s.userId) return false
      return guard.armSender(sender, s.channelId, s.userId, source, layer, dtx)
    },
    canSeal: mediaSealingSupported,
    prepareIncoming(receiver) {
      if (!receiver || !s.channelId) return false
      return guard.armReceiver(receiver, s.channelId, null)
    },
    protectIncoming(receiver, senderId, source) {
      const id = mediaUserId(senderId)
      if (!receiver || !s.channelId || !id) return false
      return guard.armReceiver(receiver, s.channelId, id, source)
    },
  }
  s.call = call
  guard.onStats(senders => handleStats(s, senders))
  work.set(call, s.tail)
  return call
}
