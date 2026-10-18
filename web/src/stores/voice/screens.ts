import { layersShape } from '../../lib/screenLayers'
import type { MediaState, ScreenLayers, ScreenLayersWanted, ScreenReception, ScreenViewers } from '../../types/events'
import type { VoiceState } from './state'
import { emitSubscription, screenRequest } from './cameras'

function syncScreenFocus(cs: VoiceState, preferred: string | null) {
  let id = preferred
  if (!id || !cs.watchedScreens.value[id]) {
    id = cs.remoteScreenUserId.value && cs.watchedScreens.value[cs.remoteScreenUserId.value]
      ? cs.remoteScreenUserId.value
      : Object.keys(cs.watchedScreens.value)[0] || null
  }
  cs.remoteScreenUserId.value = id
  cs.remoteScreenStream.value = (id && cs.remoteScreenStreams.value[id]) || null
}

// Opt into someone's screen share; it goes on the stage.
export function watchScreen(cs: VoiceState, userId: string | null) {
  if (!userId) return
  cs.ownScreenFocused.value = false
  cs.focusedCamera.value = null
  cs.watchedScreens.value = { ...cs.watchedScreens.value, [userId]: true }
  syncScreenFocus(cs, userId)
  emitSubscription(cs, screenRequest(cs, userId, true))
}

export function unwatchScreen(cs: VoiceState, userId: string | null) {
  if (!userId || !cs.watchedScreens.value[userId]) return
  const next = { ...cs.watchedScreens.value }
  delete next[userId]
  cs.watchedScreens.value = next
  forgetScreenQuality(cs, userId, false)
  const streams = { ...cs.remoteScreenStreams.value }
  delete streams[userId]
  cs.remoteScreenStreams.value = streams
  syncScreenFocus(cs, null)
  emitSubscription(cs, { kind: 'screen', user_id: userId, on: false })
}

// Which of the screen shares I watch is on the stage.
export function focusScreen(cs: VoiceState, userId: string | null) {
  if (!userId || !cs.watchedScreens.value[userId]) return
  cs.ownScreenFocused.value = false
  cs.focusedCamera.value = null
  syncScreenFocus(cs, userId)
}

export function focusOwnScreen(cs: VoiceState) {
  if (!cs.localScreenStream.value) return
  cs.ownScreenFocused.value = true
  cs.focusedCamera.value = null
}

export function setRemoteScreen(cs: VoiceState, userId: string | null, stream: MediaStream) {
  // A track that arrives after I stopped watching is not shown.
  if (!userId || !cs.watchedScreens.value[userId]) return
  cs.remoteScreenStreams.value = { ...cs.remoteScreenStreams.value, [userId]: stream }
  syncScreenFocus(cs, cs.remoteScreenUserId.value === userId || !cs.remoteScreenStream.value ? userId : null)
}

export function removeRemoteScreen(cs: VoiceState, userId: string, stream?: MediaStream) {
  const current = cs.remoteScreenStreams.value[userId]
  if (!current || (stream && current !== stream)) return
  const next = { ...cs.remoteScreenStreams.value }
  delete next[userId]
  cs.remoteScreenStreams.value = next
  syncScreenFocus(cs, null)
}

// What I chose and receive of a share ends with my watching it; the layers it
// has stay known until it ends.
function forgetScreenQuality(cs: VoiceState, userId: string, withLayers = true) {
  for (const ref of withLayers ? [cs.screenLayers, cs.receivedLayer, cs.screenChoice] as const : [cs.receivedLayer, cs.screenChoice] as const) {
    if (!(userId in ref.value)) continue
    const next = { ...ref.value }
    delete next[userId]
    ref.value = next as never
  }
}

function sameRoom(cs: VoiceState, channelId: string | undefined): boolean {
  // Late events of a room I left (or before I joined one) are not mine.
  return !!cs.currentChannelId.value && (!channelId || channelId === cs.currentChannelId.value)
}

/** screen_viewers: { channel_id, user_id (the sharer), viewers: [user IDs] }. */
export function handleScreenViewers(cs: VoiceState, event: ScreenViewers) {
  const { channel_id, user_id, viewers } = event || {}
  if (!user_id) return
  // Late events of a room I left (or before I joined one) are not mine.
  if (!cs.currentChannelId.value || (channel_id && channel_id !== cs.currentChannelId.value)) return
  const ids = Array.isArray(viewers) ? [...new Set(viewers.filter(id => id && id !== user_id))] : []
  const next = { ...cs.screenViewers.value }
  if (ids.length) next[user_id] = ids
  else delete next[user_id]
  cs.screenViewers.value = next
}

/** screen_layers: the layers of userId's live screen share ([] once it ended). */
export function handleScreenLayers(cs: VoiceState, event: ScreenLayers) {
  const { channel_id, user_id, layers } = event || {}
  if (!user_id || !sameRoom(cs, channel_id)) return
  const before = cs.screenLayers.value[user_id]
  if (!layers?.length) {
    forgetScreenQuality(cs, user_id)
    return
  }
  cs.screenLayers.value = { ...cs.screenLayers.value, [user_id]: layers }
  // A choice is a position among the layers: it survives new sizes (a resized
  // window) but not other layers, where the server's pick stands again.
  if (before && layersShape(before) !== layersShape(layers) && user_id in cs.screenChoice.value) {
    const next = { ...cs.screenChoice.value }
    delete next[user_id]
    cs.screenChoice.value = next
    if (cs.watchedScreens.value[user_id]) emitSubscription(cs, { kind: 'screen', user_id, on: true, quality: 'auto' })
  }
}

/** screen_quality: the layer of userId's share I receive now. */
export function handleScreenQuality(cs: VoiceState, event: ScreenReception) {
  const { channel_id, user_id, layer, auto } = event || {}
  if (!user_id || !sameRoom(cs, channel_id) || !cs.watchedScreens.value[user_id]) return
  cs.receivedLayer.value = { ...cs.receivedLayer.value, [user_id]: { layer, auto } }
}

/** screen_layers_wanted: the layers of my own share that someone receives. */
export function handleLayersWanted(cs: VoiceState, event: ScreenLayersWanted) {
  if (!event || !Array.isArray(event.layers) || !sameRoom(cs, event.channel_id)) return
  cs.layersWanted.value = [...event.layers]
}

/** Chooses which layer of userId's screen share I receive: 'auto' or a layer index. */
export function setScreenChoice(cs: VoiceState, userId: string | null, choice: number | 'auto') {
  if (!userId || !cs.watchedScreens.value[userId]) return
  cs.screenChoice.value = { ...cs.screenChoice.value, [userId]: choice }
  emitSubscription(cs, { kind: 'screen', user_id: userId, on: true, quality: choice })
}

/** Who watches userId's screen share (my own too): user IDs, [] for nobody. */
export function viewersOf(cs: VoiceState, userId: string | null) {
  return userId ? cs.screenViewers.value[userId] || [] : []
}

export function handleMediaState(cs: VoiceState, update: Pick<MediaState, 'user_id'> & Partial<Omit<MediaState, 'user_id'>>) {
  const { user_id, channel_id, screen, camera } = update || {}
  if (!user_id) return
  const next = { ...cs.mediaState.value }
  if ((screen || camera) && channel_id) next[user_id] = { channel_id, screen: !!screen, camera: !!camera }
  else delete next[user_id]
  cs.mediaState.value = next
  if (!screen && cs.screenViewers.value[user_id]) {
    const v = { ...cs.screenViewers.value }
    delete v[user_id]
    cs.screenViewers.value = v
  }
  if (!screen) forgetScreenQuality(cs, user_id)
  // The share ended: the opt-in ended with it.
  if (!screen && cs.watchedScreens.value[user_id]) {
    const w = { ...cs.watchedScreens.value }
    delete w[user_id]
    cs.watchedScreens.value = w
    removeRemoteScreen(cs, user_id)
    syncScreenFocus(cs, null)
  }
}

// The media connection restarted (reconnect): streams are gone and the server
// announces what is published again. Opt-ins for shares that ended are dropped.
export function resetRemoteMedia(cs: VoiceState) {
  const keep: Record<string, boolean> = {}
  for (const id of Object.keys(cs.watchedScreens.value)) {
    if (cs.mediaState.value[id]?.screen) keep[id] = true
  }
  cs.watchedScreens.value = keep
  cs.remoteScreenStreams.value = {}
  cs.mediaState.value = {}
  // The server sends who watches what again after the join.
  cs.screenViewers.value = {}
  cs.screenLayers.value = {}
  cs.receivedLayer.value = {}
  cs.layersWanted.value = null
  cs.userVideoStreams.value = {}
  syncScreenFocus(cs, null)
}

