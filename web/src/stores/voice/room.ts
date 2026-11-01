import { useAuthStore } from '../auth'
import { syncServerClock } from '../../lib/clock'
import { pageHidden } from '../../lib/pageVisibility'
import { playSoundEffect } from '../../lib/soundEffects'
import type { UserUpdate } from '../../types/domain'
import type { VoiceRooms, VoiceSnapshot, VoiceStateUpdate, MuteState, SpeakingState } from '../../types/events'
import { myUserId } from './state'
import type { VoiceState } from './state'
import { removeUserVideoStream } from './cameras'
import { resetCameraFit } from './cameraFit'
import { handleMediaState } from './screens'

export function setVoiceSnapshot(cs: VoiceState, snapshot: VoiceSnapshot | null) {
  cs.channelUsers.value = snapshot || {}
}

/** When userId joined the room they are in now ('' if in none). */
export function joinedAtOf(cs: VoiceState, userId: string | null) {
  for (const users of Object.values(cs.channelUsers.value)) {
    const user = userId ? users[userId] : undefined
    if (user) return user.joined_at || ''
  }
  return ''
}

/** voice_rooms: { started: { channelId: iso }, now: iso (server clock) }. */
export function setVoiceRooms(cs: VoiceState, payload: VoiceRooms) {
  if (payload?.now) syncServerClock(payload.now)
  cs.roomStartedAt.value = { ...(payload?.started || {}) }
}

/** The users of a room, created empty if the room is not known yet. */
function roomOf(cs: VoiceState, channelId: string) {
  if (!cs.channelUsers.value[channelId]) cs.channelUsers.value[channelId] = {}
  return cs.channelUsers.value[channelId] ?? {}
}

function currentUserId(): string | null | undefined {
  try {
    return useAuthStore()?.user?.id
  } catch {
    // outside pinia/test context
    return null
  }
}

type JoinUpdate = Extract<VoiceStateUpdate, { action: 'join' }>

function onUserJoined(cs: VoiceState, update: JoinUpdate, myId: string | null | undefined) {
  const { channel_id, user } = update
  roomOf(cs, channel_id)[user.id] = user
  if (update.started_at) cs.roomStartedAt.value = { ...cs.roomStartedAt.value, [channel_id]: update.started_at }
  if (cs.currentChannelId.value === channel_id && myId && user.id !== myId) {
    playSoundEffect('user_join')
  }
}

function pruneRoomStarted(cs: VoiceState, channel_id: string, room: Record<string, unknown>) {
  if (Object.keys(room).length === 0) {
    const next = { ...cs.roomStartedAt.value }
    delete next[channel_id]
    cs.roomStartedAt.value = next
  }
}

function shouldRetireMedia(mediaChannelId: string | undefined, channel_id: string, inActiveRoom: boolean): boolean {
  if (mediaChannelId === channel_id) return true
  if (mediaChannelId) return false
  return inActiveRoom
}

function maybePlayLeaveSound(leavesCurrentRoom: boolean, user_id: string, myId: string | null | undefined) {
  if (leavesCurrentRoom && myId && user_id !== myId) {
    playSoundEffect('user_leave')
  }
}

function onUserLeft(cs: VoiceState, channel_id: string, user_id: string, myId: string | null | undefined) {
  const room = roomOf(cs, channel_id)
  delete room[user_id]
  pruneRoomStarted(cs, channel_id, room)

  // A delayed departure from another room must not clear this room's
  // receiver streams or speaking state for a participant who moved here.
  const leavesCurrentRoom = cs.currentChannelId.value === channel_id
  const inActiveRoom = leavesCurrentRoom || !cs.currentChannelId.value
  if (inActiveRoom) {
    delete cs.speakingUsers.value[user_id]
    if (hiddenSpeaking.has(cs)) delete hiddenSpeaking.get(cs)?.[user_id]
    removeUserVideoStream(cs, user_id)
  }
  // Publication snapshots cover every room. Only retire the publication
  // named by this departure, or a current-room share without a snapshot.
  const mediaChannelId = cs.mediaState.value[user_id]?.channel_id
  if (shouldRetireMedia(mediaChannelId, channel_id, inActiveRoom)) {
    handleMediaState(cs, { user_id })
  }
  maybePlayLeaveSound(leavesCurrentRoom, user_id, myId)
}

export function handleVoiceStateUpdate(cs: VoiceState, update: VoiceStateUpdate) {
  const { action, channel_id } = update
  const user = update.action === 'join' ? update.user : null
  const user_id = update.action === 'leave' ? update.user_id : null
  roomOf(cs, channel_id)

  const myId = currentUserId()
  if (action === 'join' && user && update.action === 'join') {
    onUserJoined(cs, update, myId)
  } else if (action === 'leave' && user_id) {
    onUserLeft(cs, channel_id, user_id, myId)
  }
}

/** user_update: a new name or avatar also shows in the Talk lists and tiles. */
export function updateUser(cs: VoiceState, updated: UserUpdate) {
  if (!updated?.id) return
  for (const users of Object.values(cs.channelUsers.value)) {
    const user = users?.[updated.id]
    if (user) users[updated.id] = { ...user, ...updated }
  }
}

/** voice_mute_state: someone in a Talk muted or deafened (or stopped). */
export function handleMuteState(cs: VoiceState, event: MuteState) {
  const { channel_id, user_id, muted, deafened } = event || {}
  const users = cs.channelUsers.value[channel_id]
  const user = users?.[user_id]
  if (!users || !user) return
  users[user_id] = { ...user, muted: !!muted, deafened: !!deafened }
}

/** Mute/deafen state of a user in a Talk; mine comes from my own controls. */
export function muteStateOf(cs: VoiceState, userId: string | null) {
  if (userId && userId === myUserId()) return { muted: cs.isMuted.value, deafened: cs.isDeafened.value }
  for (const users of Object.values(cs.channelUsers.value)) {
    const u = userId ? users[userId] : undefined
    if (u) return { muted: !!u.muted, deafened: !!u.deafened }
  }
  return { muted: false, deafened: false }
}

/** The speaking ring: never while muted or deafened. */
export function isSpeaking(cs: VoiceState, userId: string | null) {
  if (!userId || !cs.speakingUsers.value[userId]) return false
  const st = muteStateOf(cs, userId)
  return !st.muted && !st.deafened
}

// Who speaks, kept up to date while the page is hidden without touching what
// is rendered; the rings get it when the page is shown again.
const hiddenSpeaking = new WeakMap<VoiceState, Record<string, boolean>>()

export function syncSpeakingVisibility(cs: VoiceState, hidden: boolean) {
  if (hidden) {
    hiddenSpeaking.set(cs, { ...cs.speakingUsers.value })
    return
  }
  const kept = hiddenSpeaking.get(cs)
  hiddenSpeaking.delete(cs)
  if (kept) cs.speakingUsers.value = kept
}

export function handleSpeakingEvent(cs: VoiceState, event: SpeakingState) {
  const { user_id, active } = event || {}
  if (!user_id) return
  const kept = pageHidden.value ? hiddenSpeaking.get(cs) : undefined
  if (kept) {
    if (active) kept[user_id] = true
    else delete kept[user_id]
    return
  }
  if (active) {
    if (!cs.speakingUsers.value[user_id]) {
      cs.speakingUsers.value = { ...cs.speakingUsers.value, [user_id]: true }
    }
  } else {
    if (cs.speakingUsers.value[user_id]) {
      const next = { ...cs.speakingUsers.value }
      delete next[user_id]
      cs.speakingUsers.value = next
    }
  }
}

export function setChannel(cs: VoiceState, channelId: string | null) {
  const prev = cs.currentChannelId.value
  cs.currentChannelId.value = channelId
  cs.isConnected.value = !!channelId
  // Viewers belong to one room.
  if (prev !== channelId) {
    cs.screenViewers.value = {}
    cs.screenLayers.value = {}
    cs.receivedLayer.value = {}
    cs.screenChoice.value = {}
    cs.screenFit.value = {}
    cs.layersWanted.value = null
    cs.speakingUsers.value = {}
    if (hiddenSpeaking.has(cs)) hiddenSpeaking.set(cs, {})
  }
  if (channelId) {
    if (prev !== channelId) {
      playSoundEffect('join')
    }
    cs.activeView.value = 'voice'
  }
}

export function disconnect(cs: VoiceState) {
  if (cs.isConnected.value || cs.currentChannelId.value) {
    playSoundEffect('leave')
  }
  cs.isMicTesting.value = false
  cs.currentChannelId.value = null
  cs.isConnected.value = false
  cs.isScreenSharing.value = false
  cs.isScreenAudioMuted.value = false
  cs.hasScreenAudio.value = false
  cs.screenQualityNeedsRestart.value = false
  resetCameraFit(cs)
  cs.cameraLayersWanted.value = null
  cs.isCameraOn.value = false
  cs.localCameraStream.value = null
  cs.remoteScreenStream.value = null
  cs.remoteScreenUserId.value = null
  cs.remoteScreenStreams.value = {}
  cs.watchedScreens.value = {}
  cs.ownScreenFocused.value = false
  cs.focusedCamera.value = null
  cs.mediaState.value = {}
  cs.screenViewers.value = {}
  cs.screenLayers.value = {}
  cs.receivedLayer.value = {}
  cs.screenChoice.value = {}
  cs.screenFit.value = {}
  cs.layersWanted.value = null
  cs.userVideoStreams.value = {}
  cs.speakingUsers.value = {}
  if (hiddenSpeaking.has(cs)) hiddenSpeaking.set(cs, {})
  cs.talkReactions.value = []
  cs.subscriptionSink = null
  cs.activeView.value = 'chat'
}

