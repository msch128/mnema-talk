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
  for (const ref of withLayers ? [cs.screenLayers, cs.receivedLayer, cs.screenChoice, cs.screenFit] as const : [cs.receivedLayer, cs.screenChoice, cs.screenFit] as const) {
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

function filterScreenViewers(viewers: unknown, sharerId: string): string[] {
  if (!Array.isArray(viewers)) return []
  return [...new Set(viewers.filter(id => Boolean(id) && id !== sharerId))]
}

/** screen_viewers: { channel_id, user_id (the sharer), viewers: [user IDs] }. */
export function handleScreenViewers(cs: VoiceState, event: ScreenViewers) {
  if (!event?.user_id || !sameRoom(cs, event.channel_id)) return
  const ids = filterScreenViewers(event.viewers, event.user_id)
  const next = { ...cs.screenViewers.value }
  if (ids.length > 0) next[event.user_id] = ids
  else delete next[event.user_id]
  cs.screenViewers.value = next
}

function resetScreenChoiceOnShapeChange(
  cs: VoiceState,
  userId: string,
  before: unknown,
  layers: unknown
) {
  if (!before || layersShape(before as never) === layersShape(layers as never)) return
  if (!(userId in cs.screenChoice.value)) return
  const next = { ...cs.screenChoice.value }
  delete next[userId]
  cs.screenChoice.value = next
  if (cs.watchedScreens.value[userId]) {
    emitSubscription(cs, { kind: 'screen', user_id: userId, on: true, quality: 'auto' })
  }
}

/** screen_layers: the layers of userId's live screen share ([] once it ended). */
export function handleScreenLayers(cs: VoiceState, event: ScreenLayers) {
  if (!event?.user_id || !sameRoom(cs, event.channel_id)) return
  const { user_id, layers } = event
  if (!layers?.length) {
    forgetScreenQuality(cs, user_id)
    return
  }
  const before = cs.screenLayers.value[user_id]
  cs.screenLayers.value = { ...cs.screenLayers.value, [user_id]: layers }
  resetScreenChoiceOnShapeChange(cs, user_id, before, layers)
}

/** screen_quality: the layer of userId's share I receive now. */
export function handleScreenQuality(cs: VoiceState, event: ScreenReception) {
  const { channel_id, user_id, layer, auto } = event || {}
  if (!user_id || !sameRoom(cs, channel_id) || !cs.watchedScreens.value[user_id]) return
  cs.receivedLayer.value = { ...cs.receivedLayer.value, [user_id]: { layer, auto } }
}

/** screen_layers_wanted: the layers of my own share (or, marked source camera, of my camera) that someone receives. */
export function handleLayersWanted(cs: VoiceState, event: ScreenLayersWanted) {
  if (!event || !Array.isArray(event.layers) || !sameRoom(cs, event.channel_id)) return
  if (event.source === 'camera') cs.cameraLayersWanted.value = [...event.layers]
  else cs.layersWanted.value = [...event.layers]
}

/** Chooses which layer of userId's screen share I receive: 'auto' or a layer index. */
export function setScreenChoice(cs: VoiceState, userId: string | null, choice: number | 'auto') {
  if (!userId || !cs.watchedScreens.value[userId]) return
  cs.screenChoice.value = { ...cs.screenChoice.value, [userId]: choice }
  emitSubscription(cs, { kind: 'screen', user_id: userId, on: true, quality: choice })
}

/**
 * Tells the server the best layer of userId's share my picture of it can use
 * (see fitLayer). Automatic stays at or below it; a chosen layer is not affected.
 */
export function setScreenFit(cs: VoiceState, userId: string | null, fit: number) {
  if (!userId || !cs.watchedScreens.value[userId] || cs.screenFit.value[userId] === fit) return
  cs.screenFit.value = { ...cs.screenFit.value, [userId]: fit }
  emitSubscription(cs, { kind: 'screen', user_id: userId, on: true, fit })
}

/** Who watches userId's screen share (my own too): user IDs, [] for nobody. */
export function viewersOf(cs: VoiceState, userId: string | null) {
  return userId ? cs.screenViewers.value[userId] || [] : []
}

function updateMediaStateEntry(
  cs: VoiceState,
  userId: string,
  update: { channel_id?: string; screen?: boolean; camera?: boolean }
) {
  const next = { ...cs.mediaState.value }
  const isActive = Boolean(update.screen || update.camera)
  if (isActive && update.channel_id) {
    next[userId] = { channel_id: update.channel_id, screen: Boolean(update.screen), camera: Boolean(update.camera) }
  } else {
    delete next[userId]
  }
  cs.mediaState.value = next
}

function cleanupEndedScreen(cs: VoiceState, userId: string) {
  if (cs.screenViewers.value[userId]) {
    const v = { ...cs.screenViewers.value }
    delete v[userId]
    cs.screenViewers.value = v
  }
  forgetScreenQuality(cs, userId)
  if (cs.watchedScreens.value[userId]) {
    const w = { ...cs.watchedScreens.value }
    delete w[userId]
    cs.watchedScreens.value = w
    removeRemoteScreen(cs, userId)
    syncScreenFocus(cs, null)
  }
}

export function handleMediaState(cs: VoiceState, update: Pick<MediaState, 'user_id'> & Partial<Omit<MediaState, 'user_id'>>) {
  if (!update?.user_id) return
  updateMediaStateEntry(cs, update.user_id, update)
  if (!update.screen) {
    cleanupEndedScreen(cs, update.user_id)
  }
}

// The media connection restarted (reconnect): streams are gone and the server
// announces what is published again. Opt-ins for shares that were live are
// kept for now; dropEndedWatches retires those the server did not announce.
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
  cs.cameraLayersWanted.value = null
  cs.userVideoStreams.value = {}
  cs.cappedCameras.value = {}
  syncScreenFocus(cs, null)
}


// Once the server has announced what is published after a reconnect: a share
// that ended during the gap is never announced, so retire its opt-in.
export function dropEndedWatches(cs: VoiceState) {
  for (const id of Object.keys(cs.watchedScreens.value)) {
    if (!cs.mediaState.value[id]?.screen) cleanupEndedScreen(cs, id)
  }
}
