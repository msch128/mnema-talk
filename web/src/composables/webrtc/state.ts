import { effectScope, ref } from 'vue'
import type { useVoiceStore } from '../../stores/voice'
import type { useChatStore } from '../../stores/chat'
import type { FilterNode } from '../../lib/dfn/dfnNode'
import type { NoiseModel } from '../../lib/dfn/dfnTypes'
import { createPushToTalk } from './pushToTalk'
import { createStatsPolling } from './statsPolling'
import { playPausedRemoteAudio } from './remoteAudio'
import type { AudioGuard } from './senderTuning'
import { setGamingPttLease } from './voiceAudio'
import type { PendingLobby } from './lobby'

export type VoiceStore = ReturnType<typeof useVoiceStore>
export type ChatStore = ReturnType<typeof useChatStore>

/** Everything one call keeps while it runs; the modules in this folder read and write it through Cx. */
export interface RtcState {
  localAudioStream: ReturnType<typeof ref<MediaStream | null>>
  localScreenStream: ReturnType<typeof ref<MediaStream | null>>
  localCameraStream: ReturnType<typeof ref<MediaStream | null>>
  audioContext: AudioContext | null
  analyser: AnalyserNode | null
  /** Always-enabled clone of the mic track that only feeds the level meter (see lib/micMetering). */
  meteringTrack: MediaStreamTrack | null
  /** With AI noise suppression the captured mic stream only feeds the model; localAudioStream is then the processed stream that is sent, gated and muted. */
  rawMicStream: MediaStream | null
  suppressorNode: FilterNode | null
  micPipelineController: AbortController | null
  micTestController: AbortController | null
  /** A failed model uses browser suppression for this session; the saved choice is untouched, and leaving/rejoining permits a fresh attempt. */
  failedAIModel: NoiseModel | null
  /** The audio context never started rendering: nothing routed through it is heard, and its meter cannot drive the gate. */
  contextStalled: boolean
  speakingInterval: ReturnType<typeof setInterval> | null
  /** Whether the others currently see my speaking ring; survives a restart of the detection so the ring can always be taken down. */
  speakingAnnounced: boolean
  /** The mic-test monitor: a gain stage to the speakers on the context it was built on (the call's or the standalone test's). */
  loopbackGain: GainNode | null
  loopbackCtx: AudioContext | null
  callBiquad: BiquadFilterNode | null
  /** Raw capture tap for the no-sound warning, read before any processing. */
  rawAnalyser: AnalyserNode | null
  /** Since when the raw capture has been silent (0: it is not). */
  noAudioStart: number
  /** The user closed the no-sound warning for this call. */
  noAudioDismissed: boolean
  /** The no-sound warning is shown once per call. */
  noAudioWarned: boolean

  pc: RTCPeerConnection | null
  /**
   * Senders of the four fixed outgoing lines, created with the connection:
   * mic, screen share, camera and the screen share's sound. Tracks are swapped
   * with replaceTrack, so starting or stopping them never renegotiates. The
   * screen's sound goes on its own line (not mixed into the mic), so viewers
   * set its volume apart from the voice and only those watching receive it.
   */
  audioSender: RTCRtpSender | null
  screenSender: RTCRtpSender | null
  cameraSender: RTCRtpSender | null
  screenAudioSender: RTCRtpSender | null
  /** Senders of the screen's lower layers (see lib/screenLayers), on the SFU's third and fourth video line; null where the server offers none. */
  layerSenders: (RTCRtpSender | null)[]
  /** Senders of the camera's lower layers (see lib/cameraLayers), on the video lines after the screen's; null where the server offers none. */
  cameraLayerSenders: (RTCRtpSender | null)[]
  /** The layers last declared to the server (lib/screenLayers.layersKey). */
  declaredKey: string
  /** The same for the camera's layers. */
  declaredCameraKey: string
  captureWatch: ReturnType<typeof setInterval> | null
  captureSettle: ReturnType<typeof setTimeout> | null
  /** A browser picker may stay open for a long time. Repeated clicks must not launch several capture pipelines; stopping also invalidates its result. */
  screenCapturePending: boolean
  screenCaptureGeneration: number
  cameraCaptureGeneration: number
  /** Quality changes are applied one after another. */
  qualityChain: Promise<void>
  /** Input volume: a gain stage in the mic pipeline, only built when the input volume is not 100 %. */
  gamingGainNode: GainNode | null
  inputGainNode: GainNode | null
  /** The settings watchers, registered once per voice store. */
  watcherScope: ReturnType<typeof effectScope> | null
  watchedStore: VoiceStore | null
  inputVolumeTimer: ReturnType<typeof setTimeout> | undefined
  /** applyAudioSettings runs one at a time; busy counts queued and running ones. */
  audioSettingsChain: Promise<void>
  audioSettingsBusy: number
  /** Offers are applied one after another; candidates that arrive before the remote description is set wait in pendingCandidates. */
  signalingChain: Promise<void>
  pendingCandidates: RTCIceCandidateInit[]
  /** Bumped by every join and leave: an older join that is still awaiting the microphone sees the change and backs out instead of finishing. */
  joinGeneration: number
  /** The join of a Create-a-Talk lobby that waits for its Talk (lobby.ts). */
  pendingLobby: PendingLobby | null
  screenAudioGuard: AudioGuard | null
  warnedScreenAudioTracks: WeakSet<MediaStreamTrack>

  /** The standalone mic test of the settings modal, used outside a call. */
  testAudioStream: MediaStream | null
  testAudioContext: AudioContext | null
  testAnalyser: AnalyserNode | null
  testSpeakingInterval: ReturnType<typeof setInterval> | null
  testBiquad: BiquadFilterNode | null
  testSuppressor: FilterNode | null
  /** Bumped whenever the standalone test is stopped or restarted (see startMicTest). */
  micTestGen: number
  /** The test installed the push-to-talk listeners (no call had them). */
  testOwnsPtt: boolean

  pushToTalk: ReturnType<typeof createPushToTalk>
  statsPolling: ReturnType<typeof createStatsPolling>
}

/** What the modules need to reach each other without importing in circles. */
interface Hooks {
  /** The noise model of an audio context failed; falls back to browser suppression and rebuilds. */
  filterFailed(audio: AudioContext, model: NoiseModel, test?: boolean): void
  /** The audio context did not start rendering; the microphone is sent around it. */
  contextStalled(audio: AudioContext): void
}

/** The context every function of the call modules receives. */
export interface Cx {
  voice: VoiceStore
  chat: ChatStore
  st: RtcState
  hooks: Hooks
}

export function createRtcState(): RtcState {
  const st: RtcState = {
    localAudioStream: ref<MediaStream | null>(null),
    localScreenStream: ref<MediaStream | null>(null),
    localCameraStream: ref<MediaStream | null>(null),
    audioContext: null,
    analyser: null,
    meteringTrack: null,
    rawMicStream: null,
    suppressorNode: null,
    micPipelineController: null,
    micTestController: null,
    failedAIModel: null,
    contextStalled: false,
    speakingInterval: null,
    speakingAnnounced: false,
    loopbackGain: null,
    loopbackCtx: null,
    callBiquad: null,
    rawAnalyser: null,
    noAudioStart: 0,
    noAudioDismissed: false,
    noAudioWarned: false,
    pc: null,
    audioSender: null,
    screenSender: null,
    cameraSender: null,
    screenAudioSender: null,
    layerSenders: [null, null], cameraLayerSenders: [null, null],
    declaredKey: '', declaredCameraKey: '',
    captureWatch: null,
    captureSettle: null,
    screenCapturePending: false,
    screenCaptureGeneration: 0,
    cameraCaptureGeneration: 0,
    qualityChain: Promise.resolve(),
    gamingGainNode: null,
    inputGainNode: null,
    watcherScope: null,
    watchedStore: null,
    inputVolumeTimer: undefined,
    audioSettingsChain: Promise.resolve(),
    audioSettingsBusy: 0,
    signalingChain: Promise.resolve(),
    pendingCandidates: [],
    joinGeneration: 0,
    pendingLobby: null,
    screenAudioGuard: null,
    warnedScreenAudioTracks: new WeakSet(),
    testAudioStream: null,
    testAudioContext: null,
    testAnalyser: null,
    testSpeakingInterval: null,
    testBiquad: null,
    testSuppressor: null,
    micTestGen: 0,
    testOwnsPtt: false,
    // Back in view, the browser may have suspended the audio it plays.
    pushToTalk: createPushToTalk({
      lease: pressed => setGamingPttLease(st, pressed),
      wake(inCall) {
        if (st.audioContext && st.audioContext.state === 'suspended') st.audioContext.resume().catch(() => {})
        if (inCall) playPausedRemoteAudio()
      }
    }),
    statsPolling: createStatsPolling({
      connection: () => st.pc,
      screenAudioGuard: () => st.screenAudioGuard,
      senders: () => [
        ['mic', st.audioSender],
        ['screenAudio', st.screenAudioSender],
        ['camera', st.cameraSender],
        ['screen', st.screenSender],
        ...st.layerSenders.map((sender): ['screen', RTCRtpSender | null] => ['screen', sender]),
        ...st.cameraLayerSenders.map((sender): ['camera', RTCRtpSender | null] => ['camera', sender])
      ]
    })
  }
  return st
}
