import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { browserMediaCall } from '../lib/media/browserCall'
import { installMediaCallFactory } from '../lib/media/api'
import type { StreamQuality } from '../lib/streamQuality'
import { onMediaUnprotected } from './webrtc/mediaRoom'
import { createRtcState, type Cx } from './webrtc/state'
import { onPageLeave } from '../lib/audioHealth'
import { recoverContext, recoverFilter, applyAudioSettings } from './webrtc/audioSettings'
import { refreshAudioDevices, teardownMicPipeline } from './webrtc/micPipeline'
import { startMicTest, stopMicTest, toggleMicTest } from './webrtc/micTest'
import { cleanupVoiceAudio, setGamingPttLease as setLease } from './webrtc/voiceAudio'
import { getScreenCaptureSettings, getScreenSendStats, replaceScreenShare, startScreenShare, stopScreenShare, toggleScreenShare } from './webrtc/screen'
import { startCamera, stopCamera, toggleCamera, restartCamera } from './webrtc/camera'
import { joinVoiceChannel, leaveVoiceChannel, rejoinAfterReconnect, resumeRemoteAudio, resumeVoiceSession } from './webrtc/session'
import { installWatchers } from './webrtc/watchers'

export { CAPTURE_POLL_MS, CAPTURE_SETTLE_MS } from './webrtc/layers'
export { CAMERA_MAX_BITRATE, SCREEN_MAX_BITRATE, screenAudioConstraints } from './webrtc/senderTuning'

// Tests install a double first. This only supplies the call when none is installed.
installMediaCallFactory(browserMediaCall)

// Shared by every component that calls useWebRTC(): one call, one state.
const rtc = createRtcState()
let leaveHooked = false

export function setGamingPttLease(pressed: boolean): void {
  setLease(rtc, pressed)
}

export function useWebRTC() {
  const cx: Cx = {
    voice: useVoiceStore(),
    chat: useChatStore(),
    st: rtc,
    hooks: {
      filterFailed: (audio, model, test) => recoverFilter(cx, audio, model, test),
      contextStalled: audio => recoverContext(cx, audio)
    }
  }
  // Leaving the page (reload, navigation) takes the workers and worklets down in
  // order, instead of the browser cancelling their module imports mid-flight.
  if (!leaveHooked) {
    leaveHooked = true
    onPageLeave(() => { stopMicTest(cx); teardownMicPipeline(cx) })
  }
  onMediaUnprotected(() => {
    stopScreenShare(cx)
    stopCamera(cx)
    cleanupVoiceAudio(cx)
  })
  installWatchers(cx)

  return {
    localAudioStream: rtc.localAudioStream,
    localScreenStream: rtc.localScreenStream,
    localCameraStream: rtc.localCameraStream,
    refreshAudioDevices: () => refreshAudioDevices(cx),
    startMicTest: () => startMicTest(cx),
    stopMicTest: () => stopMicTest(cx),
    toggleMicTest: () => toggleMicTest(cx),
    joinVoiceChannel: (channelId: string) => joinVoiceChannel(cx, channelId),
    leaveVoiceChannel: () => leaveVoiceChannel(cx),
    rejoinAfterReconnect: () => rejoinAfterReconnect(cx),
    applyAudioSettings: () => applyAudioSettings(cx),
    startScreenShare: (quality?: StreamQuality) => startScreenShare(cx, quality),
    replaceScreenShare: () => replaceScreenShare(cx),
    stopScreenShare: () => stopScreenShare(cx),
    toggleScreenShare: () => toggleScreenShare(cx),
    getScreenSendStats: () => getScreenSendStats(cx),
    getScreenCaptureSettings: () => getScreenCaptureSettings(cx),
    startCamera: () => startCamera(cx),
    stopCamera: () => stopCamera(cx),
    toggleCamera: () => toggleCamera(cx),
    restartCamera: () => restartCamera(cx),
    resumeVoiceSession: () => resumeVoiceSession(cx),
    resumeRemoteAudio: () => resumeRemoteAudio(cx)
  }
}
