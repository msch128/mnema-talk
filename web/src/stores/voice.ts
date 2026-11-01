import { defineStore } from 'pinia'
import { onScopeDispose } from 'vue'
import { onVideoSeenChange, onVisibilityChange } from '../lib/pageVisibility'
import { createVoiceState, type VoiceState } from './voice/state'
import { toggleScreenAudioMute, setScreenQuality, resetScreenQuality, openScreenShareModal, closeScreenShareModal, recordPing, setNoiseMode, toggleNoiseCancelling, toggleMute, toggleDeafen } from './voice/controls'
import { setSubscriptionSink, isCameraHidden, isCameraOffscreen, setCameraHidden, toggleCameraHidden, setAllCamerasOff, setHideNoVideo, toggleHideNoVideo, resendSubscriptions, setVideoPaused, cameraStreamOf, canFocusCamera, focusCamera, unfocusCamera, toggleCameraFocus, setUserVideoStream, removeUserVideoStream } from './voice/cameras'
import { setVisibleCameras, resetCameraVisibility, handleCamerasCapped, isCameraCapped, installCameraVisibility } from './voice/cameraVisibility'
import { watchScreen, unwatchScreen, focusScreen, focusOwnScreen, setRemoteScreen, removeRemoteScreen, handleMediaState, handleScreenViewers, handleScreenLayers, setScreenFit, handleScreenQuality, handleLayersWanted, setScreenChoice, viewersOf, resetRemoteMedia, dropEndedWatches } from './voice/screens'
import { updateUser, setVoiceRooms, syncSpeakingVisibility, joinedAtOf, setVoiceSnapshot, handleVoiceStateUpdate, handleSpeakingEvent, handleMuteState, muteStateOf, isSpeaking, setChannel, disconnect } from './voice/room'
import { setCameraSize } from './voice/cameraFit'
import { installStage } from './voice/cameras'
import { handleStageHand, handleStageTalkPower, handleTalkReaction, hasTalkPower } from './voice/stage'

export type { RTCConnectionStats } from '../lib/rtcStats'
export type { ScreenQuality } from '../lib/streamQuality'
export type { InputMode, NoiseMode, SoundEvent } from './voiceSettings'
export { USER_VOLUME_MIN, USER_VOLUME_MAX, USER_VOLUME_DEFAULT, STREAM_VOLUME_MAX, STREAM_VOLUME_DEFAULT } from '../lib/voicePlayback'
export { NOISE_MODES, SOUND_EVENTS } from './voiceSettings'

/** What the store shows of the voice state, as the refs the modules update. */
function exposedState(cs: VoiceState, stage: ReturnType<typeof installStage>) {
  return {
    currentChannelId: cs.currentChannelId,
    isMuted: cs.isMuted,
    isDeafened: cs.isDeafened,
    isScreenSharing: cs.isScreenSharing,
    isScreenAudioMuted: cs.isScreenAudioMuted,
    hasScreenAudio: cs.hasScreenAudio,
    screenQualityNeedsRestart: cs.screenQualityNeedsRestart,
    screenQuality: cs.screenQuality,
    showScreenShareModal: cs.showScreenShareModal,
    isCameraOn: cs.isCameraOn,
    localCameraStream: cs.localCameraStream,
    remoteScreenUserId: cs.remoteScreenUserId,
    remoteScreenStreams: cs.remoteScreenStreams,
    watchedScreens: cs.watchedScreens,
    mediaState: cs.mediaState,
    hiddenCameras: cs.hiddenCameras,
    allCamerasOff: cs.allCamerasOff,
    cappedCameras: cs.cappedCameras,
    hideNoVideo: cs.hideNoVideo,
    ownScreenFocused: cs.ownScreenFocused,
    focusedCamera: cs.focusedCamera,
    stage,
    screenViewers: cs.screenViewers,
    screenLayers: cs.screenLayers,
    receivedLayer: cs.receivedLayer,
    screenChoice: cs.screenChoice,
    layersWanted: cs.layersWanted,
    serverScreenLayers: cs.serverScreenLayers,
    serverCameraLayers: cs.serverCameraLayers,
    cameraLayersWanted: cs.cameraLayersWanted,
    cameraSizes: cs.cameraSizes,
    userVideoStreams: cs.userVideoStreams,
    isConnected: cs.isConnected,
    ping: cs.ping,
    pingHistory: cs.pingHistory,
    minPing: cs.minPing,
    avgPing: cs.avgPing,
    maxPing: cs.maxPing,
    rtcStats: cs.rtcStats,
    audioBlocked: cs.audioBlocked,
    showStatsModal: cs.showStatsModal,
    mediaVerification: cs.mediaVerification,
    showAudioSettings: cs.showAudioSettings,
    isPttPressed: cs.isPttPressed,
    currentInputLevel: cs.currentInputLevel,
    isMicTesting: cs.isMicTesting,
    availableInputDevices: cs.availableInputDevices,
    availableOutputDevices: cs.availableOutputDevices,
    activeView: cs.activeView,
    localScreenStream: cs.localScreenStream,
    localAudioStream: cs.localAudioStream,
    remoteScreenStream: cs.remoteScreenStream,
    channelUsers: cs.channelUsers,
    roomStartedAt: cs.roomStartedAt,
    talkReactions: cs.talkReactions,
    speakingUsers: cs.speakingUsers
  }
}

/** The module functions as store actions; each takes the state first, the store exposes them without it. */
function exposedActions(cs: VoiceState) {
  const bind = <A extends unknown[], R>(fn: (cs: VoiceState, ...args: A) => R) => (...args: A): R => fn(cs, ...args)
  return {
    toggleScreenAudioMute: bind(toggleScreenAudioMute),
    setScreenQuality: bind(setScreenQuality),
    resetScreenQuality: bind(resetScreenQuality),
    openScreenShareModal: bind(openScreenShareModal),
    closeScreenShareModal: bind(closeScreenShareModal),
    setSubscriptionSink: bind(setSubscriptionSink),
    isCameraHidden: bind(isCameraHidden),
    setCameraHidden: bind(setCameraHidden),
    toggleCameraHidden: bind(toggleCameraHidden),
    setAllCamerasOff: bind(setAllCamerasOff),
    setHideNoVideo: bind(setHideNoVideo),
    toggleHideNoVideo: bind(toggleHideNoVideo),
    resendSubscriptions: bind(resendSubscriptions),
    setVisibleCameras: bind(setVisibleCameras),
    resetCameraVisibility: bind(resetCameraVisibility),
    handleCamerasCapped: bind(handleCamerasCapped),
    isCameraCapped: bind(isCameraCapped),
    isCameraOffscreen: bind(isCameraOffscreen),
    watchScreen: bind(watchScreen),
    unwatchScreen: bind(unwatchScreen),
    focusScreen: bind(focusScreen),
    focusOwnScreen: bind(focusOwnScreen),
    cameraStreamOf: bind(cameraStreamOf),
    canFocusCamera: bind(canFocusCamera),
    focusCamera: bind(focusCamera),
    unfocusCamera: bind(unfocusCamera),
    toggleCameraFocus: bind(toggleCameraFocus),
    setRemoteScreen: bind(setRemoteScreen),
    removeRemoteScreen: bind(removeRemoteScreen),
    handleMediaState: bind(handleMediaState),
    handleScreenViewers: bind(handleScreenViewers),
    handleScreenLayers: bind(handleScreenLayers),
    handleScreenQuality: bind(handleScreenQuality),
    handleLayersWanted: bind(handleLayersWanted),
    setScreenChoice: bind(setScreenChoice),
    setScreenFit: bind(setScreenFit),
    setCameraSize: bind(setCameraSize),
    viewersOf: bind(viewersOf),
    resetRemoteMedia: bind(resetRemoteMedia),
    dropEndedWatches: bind(dropEndedWatches),
    setUserVideoStream: bind(setUserVideoStream),
    removeUserVideoStream: bind(removeUserVideoStream),
    recordPing: bind(recordPing),
    setNoiseMode: bind(setNoiseMode),
    toggleNoiseCancelling: bind(toggleNoiseCancelling),
    updateUser: bind(updateUser),
    setVoiceRooms: bind(setVoiceRooms),
    joinedAtOf: bind(joinedAtOf),
    setVoiceSnapshot: bind(setVoiceSnapshot),
    handleVoiceStateUpdate: bind(handleVoiceStateUpdate),
    handleSpeakingEvent: bind(handleSpeakingEvent),
    handleMuteState: bind(handleMuteState),
    muteStateOf: bind(muteStateOf),
    handleStageHand: bind(handleStageHand),
    handleStageTalkPower: bind(handleStageTalkPower),
    handleTalkReaction: bind(handleTalkReaction),
    hasTalkPower: bind(hasTalkPower),
    isSpeaking: bind(isSpeaking),
    toggleMute: bind(toggleMute),
    toggleDeafen: bind(toggleDeafen),
    setChannel: bind(setChannel),
    disconnect: bind(disconnect)
  }
}

export const useVoiceStore = defineStore('voice', () => {
  const cs = createVoiceState()
  const stage = installStage(cs)
  installCameraVisibility(cs)
  // A hidden page shows no video: the server stops sending it (audio goes on) and starts again when the page is back.
  // (Picture-in-Picture counts as seen: it is used exactly when the tab is in the background.)
  onScopeDispose(onVideoSeenChange(unseen => { if (cs.currentChannelId.value) setVideoPaused(cs, unseen) }))
  onScopeDispose(onVisibilityChange(hidden => syncSpeakingVisibility(cs, hidden)))
  return { ...exposedState(cs, stage), ...exposedActions(cs), ...cs.settings, ...cs.playback }
})

export type VoiceStore = ReturnType<typeof useVoiceStore>
