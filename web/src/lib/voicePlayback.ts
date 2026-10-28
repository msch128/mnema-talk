// What I hear of whom: per-user voice volume and mute, and the volume of
// someone's screen share sound. Browser-only settings (localStorage), used by
// the voice store.
import { ref } from 'vue'
import { isRecord } from '../types/validation'

// Per-user playback settings live in the browser only (what I hear of whom).
const USER_VOLUMES_KEY = 'mnema_user_volumes'
const USER_MUTED_KEY = 'mnema_user_muted'
// The sound of someone's screen share has its own volume (0..100 %), apart
// from their voice.
const STREAM_VOLUMES_KEY = 'mnema_stream_volumes'
export const USER_VOLUME_MIN = 0
export const USER_VOLUME_MAX = 200
export const USER_VOLUME_DEFAULT = 100
export const STREAM_VOLUME_MAX = 100
// Someone's stream starts at half volume; a changed volume is remembered.
export const STREAM_VOLUME_DEFAULT = 50

/** A stored string, or null when there is none or the storage is blocked. */
export function readItem(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch (error) {
    void error
    return null
  }
}

/** Stores a string; when storage is blocked or full the setting only lasts for this session. */
export function writeItem(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch (error) {
    void error
  }
}

export function readJson(key: string): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(readItem(key) || '{}')
    return isRecord(parsed) ? parsed : {}
  } catch (error) {
    void error
    return {}
  }
}

export function writeJson(key: string, value: Record<string, unknown>) {
  writeItem(key, JSON.stringify(value))
}

/** Per-user playback (volume 0..200 %, local mute). */
function useUserPlayback() {
  const userVolumes = ref(readJson(USER_VOLUMES_KEY))
  const localMutedUsers = ref(readJson(USER_MUTED_KEY))

  function getUserVolume(userId: string | null) {
    const v = userId ? userVolumes.value[userId] : undefined
    return typeof v === 'number' ? v : USER_VOLUME_DEFAULT
  }

  function setUserVolume(userId: string | null, volume: number | string) {
    if (!userId) return
    const n = Number(volume)
    if (!Number.isFinite(n)) return
    const clamped = Math.min(USER_VOLUME_MAX, Math.max(USER_VOLUME_MIN, Math.round(n)))
    const next = { ...userVolumes.value }
    // The default needs no entry.
    if (clamped === USER_VOLUME_DEFAULT) delete next[userId]
    else next[userId] = clamped
    userVolumes.value = next
    writeJson(USER_VOLUMES_KEY, next)
  }

  function isUserLocalMuted(userId: string | null) {
    return !!userId && !!localMutedUsers.value[userId]
  }

  function toggleLocalMute(userId: string | null) {
    if (!userId) return
    const next = { ...localMutedUsers.value }
    if (next[userId]) delete next[userId]
    else next[userId] = true
    localMutedUsers.value = next
    writeJson(USER_MUTED_KEY, next)
  }

  return { userVolumes, localMutedUsers, getUserVolume, setUserVolume, isUserLocalMuted, toggleLocalMute }
}

/** The sound of someone's screen share (0..100 %), apart from their voice. */
function useStreamPlayback() {
  const streamVolumes = ref(readJson(STREAM_VOLUMES_KEY))
  // Muted streams, for this session; the volume is kept for unmuting.
  const mutedStreams = ref<Record<string, boolean>>({})

  function getStreamVolume(userId: string | null) {
    const v = userId ? streamVolumes.value[userId] : undefined
    return typeof v === 'number' ? v : STREAM_VOLUME_DEFAULT
  }

  function unmuteStream(userId: string) {
    const m = { ...mutedStreams.value }
    delete m[userId]
    mutedStreams.value = m
  }

  // Moving the slider also unmutes, like Discord.
  function setStreamVolume(userId: string | null, volume: number | string) {
    if (!userId) return
    const n = Number(volume)
    if (!Number.isFinite(n)) return
    const clamped = Math.min(STREAM_VOLUME_MAX, Math.max(0, Math.round(n)))
    const next = { ...streamVolumes.value, [userId]: clamped }
    streamVolumes.value = next
    writeJson(STREAM_VOLUMES_KEY, next)
    if (mutedStreams.value[userId]) unmuteStream(userId)
  }

  /** Silent: muted, or the volume all the way down. */
  function isStreamMuted(userId: string | null) {
    return (!!userId && !!mutedStreams.value[userId]) || getStreamVolume(userId) === 0
  }

  function toggleStreamMute(userId: string | null) {
    if (!userId) return
    if (!isStreamMuted(userId)) {
      mutedStreams.value = { ...mutedStreams.value, [userId]: true }
      return
    }
    // Unmuting a stream turned all the way down brings it back to the default.
    if (getStreamVolume(userId) === 0) setStreamVolume(userId, STREAM_VOLUME_DEFAULT)
    unmuteStream(userId)
  }

  /** What the stage's slider shows: 0 while muted. */
  function streamVolumeShown(userId: string | null) {
    return userId && mutedStreams.value[userId] ? 0 : getStreamVolume(userId)
  }

  /** Linear gain for the sound of userId's screen share (before the master volume). */
  function streamGain(userId: string | null) {
    return isStreamMuted(userId) ? 0 : getStreamVolume(userId) / 100
  }

  return { streamVolumes, mutedStreams, getStreamVolume, setStreamVolume, isStreamMuted, toggleStreamMute, streamVolumeShown, streamGain }
}

export function usePlaybackPrefs() {
  return { ...useUserPlayback(), ...useStreamPlayback() }
}
