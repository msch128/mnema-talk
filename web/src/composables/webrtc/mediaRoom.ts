// The call's media group (see lib/media) talks to the room through this: its
// handshake wires go out over the WebSocket, and its two warnings reach the
// member once per call.
import { t } from '../../i18n'
import type { MediaNotice } from '../../lib/media/api'
import { useChatStore } from '../../stores/chat'
import { useToastStore } from '../../stores/toast'
import { useVoiceStore } from '../../stores/voice'

// The hub relays a handshake only to members of the voice room, and drops one
// that arrives before voice_join. The call starts while the microphone is still
// opening, so its first wires wait here until voice_join is sent.
let roomOpen = false
const heldWires: { channelId: string, wire: string }[] = []

function sendWire(channelId: string, wire: string) {
  if (useVoiceStore().currentChannelId !== channelId) return
  useChatStore().sendWSEvent('media_handshake', { channel_id: channelId, wire })
}

export function queueMediaWire(channelId: string, wire: string) {
  if (roomOpen) sendWire(channelId, wire)
  else heldWires.push({ channelId, wire })
}

export function openMediaRoom() {
  roomOpen = true
  for (const held of heldWires.splice(0)) sendWire(held.channelId, held.wire)
}

export function closeMediaRoom() {
  roomOpen = false
  heldWires.length = 0
}

let unprotectedAnnounced = false
let rekeyAnnounced = false
let outdatedAnnounced = false
let connectionAnnounced = false
let releaseUnprotected: () => void = () => {}

/** What to stop when the call turns out to carry media unprotected. */
export function onMediaUnprotected(release: () => void) {
  releaseUnprotected = release
}

/** True once this call has been found unprotected: nothing new may start on it. */
export function mediaUnprotected() {
  return unprotectedAnnounced
}

/** A new call, or the end of one, starts the warnings over. */
export function resetMediaAnnouncements() {
  unprotectedAnnounced = false
  rekeyAnnounced = false
  outdatedAnnounced = false
  connectionAnnounced = false
}

// The group dropped this member without being asked: whoever relays the call
// can do that, so say so once per call.
export function announceMediaRekeyed(notice: MediaNotice) {
  if (notice === 'outdated') return announceOutdatedClient()
  if (rekeyAnnounced) return
  rekeyAnnounced = true
  // A member dropped after its own browser slept was not removed by anyone hostile.
  useToastStore().info(t(notice === 'absent' ? 'voice.mediaRefreshed' : 'voice.mediaRekeyed'))
}

// Someone's client still seals media in the retired frame format: it is
// dropped, so say once per call why that member is silent.
function announceOutdatedClient() {
  if (outdatedAnnounced) return
  outdatedAnnounced = true
  useToastStore().error(t('voice.mediaOutdatedClient'))
}

export function announceMediaUnprotected() {
  if (unprotectedAnnounced) return
  unprotectedAnnounced = true
  releaseUnprotected()
  useToastStore().error(t('voice.mediaUnprotected'))
}

// How long the "no media connection" notice stays: it explains something the
// member cannot fix alone, so it outlives the usual five seconds.
const CONNECTION_NOTICE_MS = 20_000

/** True once this call has told the member that no media connection came up. */
export function mediaConnectionAnnounced() {
  return connectionAnnounced
}

/**
 * The browser could not reach the voice server (ICE failed): once per call,
 * say that the network is the likely cause. Without a TURN relay on offer the
 * server's operator is the one who can help, so the notice says so.
 */
export function announceNoMediaConnection(relayOffered: boolean) {
  if (connectionAnnounced) return
  connectionAnnounced = true
  useToastStore().push({ type: 'info', text: t(relayOffered ? 'voice.noMediaConnectionRelay' : 'voice.noMediaConnection'), duration: CONNECTION_NOTICE_MS })
}
