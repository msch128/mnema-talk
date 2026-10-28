// Playing the other members' audio: one element per remote track, its volume
// from the voice store, an optional gain stage above 100 %, the output device.
import { applyOutputDevice, type AudioOutputTarget } from '../../lib/audioOutput'
import type { useVoiceStore } from '../../stores/voice'
import { SCREEN_AUDIO_STREAM_PREFIX } from './streamIds'

type VoiceStore = ReturnType<typeof useVoiceStore>
type RemoteAudioElement = HTMLAudioElement & { srcObject: MediaStream | null }

// Remote audio elements by track id. The SFU reuses its transceivers: the
// same track may come back in a new stream (another speaker), so elements
// are keyed by track and retagged instead of piling up.
const remoteAudioElements = new Map<string, RemoteAudioElement>()
const boosts = new WeakMap<RemoteAudioElement, { source: MediaStreamAudioSourceNode; gain: GainNode }>()
// Playback above 100 % needs a gain stage: element.volume tops out at 1.
let playbackContext: AudioContext | null = null
// The voice store, for the output device of a lazily created playback context.
let outputDeviceStore: VoiceStore | null = null

// Autoplay, a closed device, or a detached element: the next gesture retries.
function ignorePlaybackError(): void { /* playback retry is the caller's next gesture */ }

// Linear gain for one remote audio element: master volume x that user's
// voice volume, or x the volume of their stream for a screen share's sound.
function remoteGain(voiceStore: VoiceStore, el: RemoteAudioElement) {
  if (voiceStore.isDeafened || voiceStore.isMicTesting) return 0
  const master = voiceStore.outputVolume / 100
  const userId = el.dataset?.userId
  if (!userId) return master
  if (el.dataset.source === 'screen') return master * voiceStore.streamGain(userId)
  if (voiceStore.isUserLocalMuted(userId)) return 0
  return master * (voiceStore.getUserVolume(userId) / 100)
}

// Routes the element's stream through a GainNode (the element stays attached
// but muted so the stream keeps flowing). Only done once a gain above 1 is
// asked for; until then plain element.volume does the job.
function audioContextCtor(): typeof AudioContext | undefined {
  if (typeof window === 'undefined') return undefined
  if (window.AudioContext) return window.AudioContext
  return window.webkitAudioContext
}

function openPlayback(AudioCtx: typeof AudioContext): AudioContext {
  if (!playbackContext || playbackContext.state === 'closed') {
    playbackContext = new AudioCtx()
    if (outputDeviceStore) applyOutput(outputDeviceStore, playbackContext)
  }
  if (playbackContext.state === 'suspended') holdForGesture(playbackContext)
  return playbackContext
}

// The boosted elements are muted, so their play() always resolves and never
// reports autoplay: a context that stays suspended (no user gesture yet) would
// leave the call silent without the "click to turn the sound on" banner.
function holdForGesture(ctx: AudioContext) {
  const store = outputDeviceStore
  if (store) store.audioBlocked = true
  ctx.resume().then(() => {
    if (store && ctx.state === 'running' && ctx === playbackContext) store.audioBlocked = false
  }).catch(() => { ignorePlaybackError() })
}

function createBoost(el: RemoteAudioElement) {
  const AudioCtx = audioContextCtor()
  if (!AudioCtx) return null
  if (!el.srcObject) return null
  try {
    const ctx = openPlayback(AudioCtx)
    const source = ctx.createMediaStreamSource(el.srcObject)
    const gain = ctx.createGain()
    source.connect(gain)
    gain.connect(ctx.destination)
    el.muted = true
    const boost = { source, gain }
    boosts.set(el, boost)
    return boost
  } catch (err) {
    console.warn('[WebRTC] Volume boost unavailable:', err)
    return null
  }
}

// Plays remote audio (an element or an AudioContext) on the chosen output device.
export function applyOutput(voiceStore: VoiceStore, target: AudioOutputTarget | null) {
  applyOutputDevice(target, voiceStore.selectedOutputDeviceId)
}

/** The store whose output device a lazily created playback context follows. */
export function followOutputDevice(voiceStore: VoiceStore) {
  outputDeviceStore = voiceStore
}

/** The chosen output device changed: the voices, and the boosted ones in the playback context, move to it. */
export function applyOutputToRemote(voiceStore: VoiceStore) {
  remoteAudioElements.forEach(el => applyOutput(voiceStore, el))
  applyOutput(voiceStore, playbackContext)
}

function applyRemoteGain(voiceStore: VoiceStore, el: RemoteAudioElement) {
  const gain = remoteGain(voiceStore, el)
  let boost = boosts.get(el) ?? null
  if (!boost && gain > 1) boost = createBoost(el)
  if (boost) {
    boost.gain.gain.value = gain
  } else {
    el.volume = Math.min(1, Math.max(0, gain))
  }
}

function releaseBoost(el: RemoteAudioElement) {
  const boost = boosts.get(el)
  if (!boost) return
  try {
    boost.source.disconnect()
    boost.gain.disconnect()
  } catch {
    ignorePlaybackError()
  }
  boosts.delete(el)
}

export function updateRemoteVolume(voiceStore: VoiceStore) {
  remoteAudioElements.forEach(el => applyRemoteGain(voiceStore, el))
}

/** Plays what the browser paused while the page was hidden. */
export function playPausedRemoteAudio() {
  remoteAudioElements.forEach(el => { if (el.paused) el.play().catch(() => { ignorePlaybackError() }) })
}

/** After a click: autoplay was blocked once, so start everything again and note what stays blocked. */
export function resumeRemoteAudio(voiceStore: VoiceStore) {
  playbackContext?.resume?.().catch(() => { ignorePlaybackError() })
  remoteAudioElements.forEach(el => el.play().catch(() => { voiceStore.audioBlocked = true }))
}

/** The call is over: every element and the playback context go. */
export function releaseRemoteAudio() {
  remoteAudioElements.forEach(el => {
    releaseBoost(el)
    try {
      el.pause()
      el.srcObject = null
      el.remove()
    } catch {
      ignorePlaybackError()
    }
  })
  remoteAudioElements.clear()
  if (playbackContext) {
    playbackContext.close().catch(() => { ignorePlaybackError() })
    playbackContext = null
  }
}

function removeRemoteAudio(trackId: string) {
  const el = remoteAudioElements.get(trackId)
  if (!el) return
  remoteAudioElements.delete(trackId)
  releaseBoost(el)
  try {
    el.pause?.()
    el.srcObject = null
    el.remove()
  } catch {
    ignorePlaybackError()
  }
}

// Who an audio element plays (data-user-id) and whether it is the sound of
// their screen share (data-source="screen") rather than their voice.
function tagRemoteAudio(el: RemoteAudioElement, streamId: string) {
  const screen = streamId.startsWith(SCREEN_AUDIO_STREAM_PREFIX)
  const userId = screen ? streamId.slice(SCREEN_AUDIO_STREAM_PREFIX.length) : streamId
  if (userId) el.dataset.userId = userId
  else delete el.dataset.userId
  if (screen) el.dataset.source = 'screen'
  else delete el.dataset.source
  el.dataset.streamId = streamId
}

// Plays one remote audio track. The stream ID names the speaking user (or,
// prefixed, the user whose screen share it is); a repeated ontrack for the
// same track (transceiver reused for someone else) only retags its element.
function streamIdOf(stream: MediaStream | undefined): string {
  if (!stream?.id) return ''
  return stream.id
}

function retagPlaying(voiceStore: VoiceStore, el: RemoteAudioElement, streamId: string) {
  tagRemoteAudio(el, streamId)
  applyRemoteGain(voiceStore, el)
  if (!el.paused) return
  el.play?.().catch(() => { ignorePlaybackError() })
}

function trackStillHere(el: RemoteAudioElement, streamId: string, track: MediaStreamTrack): boolean {
  const tagged = el.dataset.streamId || ''
  if (tagged !== streamId) return false
  return el.srcObject?.getAudioTracks()[0] === track
}

function dropRemovedTrack(track: MediaStreamTrack, streamId: string, event: MediaStreamTrackEvent) {
  if (event.track !== track) return
  const el = remoteAudioElements.get(track.id)
  if (!el) return
  if (!trackStillHere(el, streamId, track)) return
  removeRemoteAudio(track.id)
}

// When the SFU stops forwarding, the browser removes the track from its
// stream instead of ending it. Only drop the element while it still
// belongs to this stream: a reused track may already play for someone else.
function watchRemoval(stream: MediaStream | undefined, track: MediaStreamTrack, streamId: string) {
  stream?.addEventListener?.('removetrack', (event) => { dropRemovedTrack(track, streamId, event) })
}

export function attachRemoteAudio(voiceStore: VoiceStore, track: MediaStreamTrack, stream: MediaStream | undefined) {
  const streamId = streamIdOf(stream)
  removeSupersededAudio(track, streamId)
  const existing = remoteAudioElements.get(track.id)
  if (existing) retagPlaying(voiceStore, existing, streamId)
  else startRemoteAudio(voiceStore, track, streamId)
  watchRemoval(stream, track, streamId)
}

// A source can return on a different receiver track after renegotiation.
// Its previous HTML/WebAudio sink must leave before the replacement plays;
// microphone and screen audio stay distinct through their stream prefixes.
function removeSupersededAudio(track: MediaStreamTrack, streamId: string) {
  for (const [trackId, el] of remoteAudioElements) {
    const sameSource = streamId && el.dataset.streamId === streamId
    const replacedTrack = trackId === track.id && el.srcObject?.getAudioTracks()[0] !== track
    if ((sameSource && trackId !== track.id) || replacedTrack) removeRemoteAudio(trackId)
  }
}

function audioSink(): HTMLElement | null {
  if (typeof document === 'undefined' || !document.body) return null
  let sink = document.getElementById('mnema-audio-sink')
  if (!sink) {
    sink = document.createElement('div')
    sink.id = 'mnema-audio-sink'
    sink.style.display = 'none'
    document.body.appendChild(sink)
  }
  return sink
}

function startRemoteAudio(voiceStore: VoiceStore, track: MediaStreamTrack, streamId: string) {
  const audioEl = new Audio() as RemoteAudioElement
  audioEl.srcObject = new MediaStream([track])
  audioEl.autoplay = true
  tagRemoteAudio(audioEl, streamId)
  applyOutput(voiceStore, audioEl)
  applyRemoteGain(voiceStore, audioEl)
  audioEl.play().catch(e => {
    // After a reload without a user gesture the browser may refuse to play;
    // the UI then offers a click to enable sound (resumeRemoteAudio).
    if (e?.name === 'NotAllowedError') voiceStore.audioBlocked = true
    console.warn('[WebRTC] Audio auto-play warning:', e)
  })
  audioSink()?.appendChild(audioEl)
  remoteAudioElements.set(track.id, audioEl)
  track.onended = () => {
    const current = remoteAudioElements.get(track.id)
    if (current?.srcObject?.getAudioTracks()[0] === track) removeRemoteAudio(track.id)
  }
}
