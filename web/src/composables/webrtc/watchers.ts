import { effectScope, watch } from 'vue'
import { mediaCall } from '../../lib/media/api'
import { applyOutput, applyOutputToRemote, followOutputDevice, updateRemoteVolume } from './remoteAudio'
import { announceMediaRekeyed, queueMediaWire } from './mediaRoom'
import type { Cx } from './state'
import { setNodeGain } from './audioMath'
import { applyAudioSettings } from './audioSettings'
import { syncCameraLayers } from './cameraLayerSync'
import { syncLayers } from './layers'
import { applyScreenQuality, liveScreenAudioTrack } from './senders'
import { sendMuteState } from './session'
import { setGamingPttLease } from './voiceAudio'

export function installWatchers(cx: Cx) {
  const { voice, st } = cx
  // These watchers exist once per voice store, however many components call
  // useWebRTC(): one per caller would send the mute state and rebuild the
  // microphone several times over. They live in a detached scope, so they
  // keep running after the first caller unmounts; a new store (a new Pinia,
  // as in tests) replaces them.
  if (st.watchedStore !== voice) {
    st.watcherScope?.stop()
    st.watchedStore = voice
    followOutputDevice(voice)
    st.watcherScope = effectScope(true)
    st.watcherScope.run(() => {
      installAudioWatchers(cx)
      installMediaCallWatchers(cx)
    })
  }
}

function installAudioWatchers(cx: Cx) {
  const { voice, st } = cx
  watch(() => voice.selectedOutputDeviceId, () => {
    applyOutputToRemote(voice)
    // The mic-test monitor.
    applyOutput(voice, st.loopbackCtx)
  })

  // Input volume: adjust the gain live; the first change away from 100 %
  // rebuilds the pipeline once to put the gain stage in.
  watch(() => voice.inputMode, () => { voice.isPttPressed = false; setGamingPttLease(st, false) }, { flush: 'sync' })
  watch(() => voice.inputVolume, (v) => {
    if (st.inputGainNode) {
      setNodeGain(st.inputGainNode, v / 100, st.audioContext?.currentTime ?? 0)
      return
    }
    if (v === 100 || !st.localAudioStream.value) return
    clearTimeout(st.inputVolumeTimer)
    st.inputVolumeTimer = setTimeout(() => applyAudioSettings(cx), 250)
  })

  watch([
    () => voice.outputVolume,
    () => voice.isDeafened,
    () => voice.userVolumes,
    () => voice.localMutedUsers,
    () => voice.streamVolumes,
    () => voice.mutedStreams
  ], () => {
    updateRemoteVolume(voice)
  })

  // The streamer's mute of their own share's sound: silence on its line.
  watch(() => voice.isScreenAudioMuted, (muted) => {
    const track = liveScreenAudioTrack(cx)
    if (track) track.enabled = !muted
  })

  // The chosen stream quality, applied to the running share.
  watch(() => voice.screenQuality, () => { void applyScreenQuality(cx).then(() => syncLayers(cx)) })
  // The layers viewers need: the others stop being encoded and sent.
  watch(() => voice.layersWanted, () => { void syncLayers(cx) })
  // The same for the camera's layers.
  watch(() => voice.cameraLayersWanted, () => { void syncCameraLayers(cx) })

  // Everyone sees whether I am muted or deafened.
  watch(() => [voice.isMuted, voice.isDeafened], () => sendMuteState(cx))
}

function installMediaCallWatchers(cx: Cx) {
  const { voice, chat } = cx
  const call = mediaCall()
  call.onKey(() => {
    if (!voice.currentChannelId) return
    chat.sendWSEvent('webrtc_request_keyframe', {})
  })
  call.onWire((channelId, wire) => {
    queueMediaWire(channelId, wire)
  })
  call.onNotice(announceMediaRekeyed)
  call.onVerification((verification) => { voice.mediaVerification = verification })
  watch(() => {
    const channelId = voice.currentChannelId
    if (!channelId) return ''
    return Object.keys(voice.channelUsers[channelId] || {}).sort().join('\0')
  }, (packed) => {
    mediaCall().roster(packed ? packed.split('\0') : [])
  })
}
