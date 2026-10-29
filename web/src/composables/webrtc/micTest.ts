import { t } from '../../i18n'
import { useToastStore } from '../../stores/toast'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported } from '../../lib/noiseSuppressor'
import { createVoiceGate } from '../../lib/levelMeter'
import { updateRemoteVolume } from './remoteAudio'
import type { Cx } from './state'
import { aiModel, calculateRMSLevel, setNodeGain } from './audioMath'
import { connectLoopback, disconnectLoopback, micErrorKey, refreshAudioDevices, requestMicrophone } from './micPipeline'

type Stale = () => boolean

async function acquireTestStream(cx: Cx, stale: Stale) {
  const { voice } = cx
  let stream
  try {
    stream = await requestMicrophone(cx)
  } catch (err) {
    console.warn('Mic test failed (permission denied or no device):', err)
    if (!stale()) useToastStore().error(t(micErrorKey(err)))
    if (!stale()) voice.currentInputLevel = 0
    if (!stale()) voice.isMicTesting = false
    return null
  }
  if (stale()) {
    stream.getTracks().forEach(tr => tr.stop())
    return null
  }
  return stream
}

// Meter the filtered signal so the gate threshold calibrates like in a call.
// Resolves to the node the meter listens to, or null when the test was superseded.
async function filteredTestSource(cx: Cx, ctx: AudioContext, input: AudioNode, controller: AbortController, stale: Stale) {
  const { voice, st } = cx
  const model = aiModel(voice.noiseMode)
  if (!model || model === st.failedAIModel || !isNoiseSuppressionSupported()) return input
  const node = await createNoiseSuppressorNode(ctx, model, { signal: controller.signal, onFailure: () => cx.hooks.filterFailed(ctx, model, true) })
  if (stale()) {
    // stopMicTest already closed the context and stopped the stream.
    try { node?.destroy?.() } catch (err) { console.debug?.('Filter destroy error:', err) }
    return null
  }
  if (!node) {
    cx.hooks.filterFailed(ctx, model, true)
    return input
  }
  input.connect(node)
  st.testSuppressor = node
  return node
}

// Drives the meter and the loopback gate while the test runs.
function startTestMeter(cx: Cx, ctx: AudioContext, analyserNode: AnalyserNode) {
  const { voice, st } = cx
  const buffer = new Uint8Array(analyserNode.fftSize)
  const gateOpen = createVoiceGate(voice)
  st.testSpeakingInterval = setInterval(() => {
    if (!st.testAnalyser) return
    const level = calculateRMSLevel(st.testAnalyser, buffer)
    voice.currentInputLevel = level
    const open = gateOpen(level)
    if (st.loopbackGain && st.loopbackCtx === ctx && voice.isMicTesting) {
      setNodeGain(st.loopbackGain, open ? (voice.outputVolume / 100) : 0, ctx.currentTime)
    }
  }, 50)
}

async function buildTestPipeline(cx: Cx, stream: MediaStream, stale: Stale) {
  const { voice, st } = cx
  const AudioCtx = window.AudioContext || window.webkitAudioContext
  const ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
  st.testAudioContext = ctx
  const controller = new AbortController()
  st.micTestController = controller
  // Not awaited: without a user gesture resume() may never settle.
  if (ctx.state === 'suspended') ctx.resume().catch(() => {})

  const analyserNode = ctx.createAnalyser()
  analyserNode.fftSize = 256
  analyserNode.smoothingTimeConstant = 0.2

  const source = await filteredTestSource(cx, ctx, ctx.createMediaStreamSource(stream), controller, stale)
  if (!source) return false
  const biquad = ctx.createBiquadFilter()
  biquad.type = 'highpass'
  biquad.frequency.setValueAtTime(85, ctx.currentTime)
  source.connect(biquad)
  biquad.connect(analyserNode)
  st.testAnalyser = analyserNode
  st.testBiquad = biquad

  if (voice.isMicTesting) connectLoopback(cx, ctx, biquad)

  // Push-to-talk works in the test too, also outside a call.
  if (!st.pushToTalk.attached()) {
    st.pushToTalk.attach(voice)
    st.testOwnsPtt = true
  }

  startTestMeter(cx, ctx, analyserNode)

  // Refresh devices after permission is granted so device labels are available
  await refreshAudioDevices(cx)
  return !stale()
}

// The standalone test (outside a call). Every await is followed by a check
// of micTestGen: stopMicTest (or a newer start) bumps it, and a superseded
// start cleans up what it acquired instead of finishing, so closing the
// settings while the browser still asks for the mic leaves nothing open.
export async function startMicTest(cx: Cx) {
  const { voice, st } = cx
  // If we're already connected to a voice channel, ensure audioContext is active
  if (st.localAudioStream.value && st.audioContext) {
    if (st.audioContext.state === 'suspended') {
      st.audioContext.resume().catch(() => {})
    }
    return true
  }

  teardownTestPipeline(cx)
  const gen = st.micTestGen
  const stale = () => gen !== st.micTestGen

  const stream = await acquireTestStream(cx, stale)
  if (!stream) return false
  st.testAudioStream = stream

  try {
    return await buildTestPipeline(cx, stream, stale)
  } catch (err) {
    console.warn('Mic test setup failed:', err)
    if (!stale()) {
      teardownTestPipeline(cx)
      voice.currentInputLevel = 0
      voice.isMicTesting = false
    }
    return false
  }
}

/** null: not in a call. Otherwise whether the call loopback connected. */
function startCallLoopback(cx: Cx): boolean | null {
  const { voice, chat, st } = cx
  if (!st.localAudioStream.value || !st.audioContext || !st.callBiquad) return null
  connectLoopback(cx, st.audioContext, st.callBiquad)
  const track = st.localAudioStream.value.getAudioTracks()[0]
  if (track) track.enabled = false
  st.speakingAnnounced = false
  chat.sendWSEvent('voice_speaking', { active: false })
  updateRemoteVolume(voice)
  return !!st.loopbackGain
}

async function startStandaloneLoopback(cx: Cx): Promise<boolean> {
  const { voice, st } = cx
  if (st.testAudioContext && st.testBiquad) {
    connectLoopback(cx, st.testAudioContext, st.testBiquad)
    return !!st.loopbackGain
  }
  const starting = startMicTest(cx)
  const gen = st.micTestGen
  const started = await starting
  if (gen !== st.micTestGen) return false
  if (!started || !st.loopbackGain) voice.isMicTesting = false
  return started && !!st.loopbackGain
}

async function startMicLoopback(cx: Cx): Promise<boolean> {
  disconnectLoopback(cx)
  cx.voice.isMicTesting = true
  const inCall = startCallLoopback(cx)
  if (inCall !== null) return inCall
  return startStandaloneLoopback(cx)
}

function stopMicLoopback(cx: Cx) {
  const { voice, st } = cx
  voice.isMicTesting = false
  disconnectLoopback(cx)
  if (st.localAudioStream.value) {
    const track = st.localAudioStream.value.getAudioTracks()[0]
    if (track) track.enabled = !voice.isMuted
    updateRemoteVolume(voice)
  }
}

export function toggleMicTest(cx: Cx) {
  const { voice } = cx
  if (voice.isMicTesting) { stopMicTest(cx); return Promise.resolve(false) }
  return startMicLoopback(cx)
}

function teardownTestControllers(cx: Cx) {
  const { st } = cx
  st.micTestGen++
  st.micTestController?.abort()
  st.micTestController = null
  if (st.testSpeakingInterval) {
    clearInterval(st.testSpeakingInterval)
    st.testSpeakingInterval = null
  }
  if (st.loopbackCtx && st.loopbackCtx === st.testAudioContext) disconnectLoopback(cx)
  if (st.testOwnsPtt) {
    st.testOwnsPtt = false
    // A call that started meanwhile keeps its own listeners.
    if (!st.localAudioStream.value) st.pushToTalk.detach()
  }
}

function teardownTestAudio(cx: Cx) {
  const { st } = cx
  if (st.testSuppressor) {
    try {
      st.testSuppressor.disconnect?.()
      st.testSuppressor.destroy?.()
    } catch (err) {
      console.debug?.('Test suppressor disconnect error:', err)
    }
    st.testSuppressor = null
  }
  st.testBiquad = null
  st.testAnalyser = null
  if (st.testAudioStream) {
    st.testAudioStream.getTracks().forEach(tr => tr.stop())
    st.testAudioStream = null
  }
  if (st.testAudioContext) {
    st.testAudioContext.close().catch(() => {})
    st.testAudioContext = null
  }
}

// Ends the standalone test pipeline (and any start still awaiting the mic).
function teardownTestPipeline(cx: Cx) {
  teardownTestControllers(cx)
  teardownTestAudio(cx)
}

export function stopMicTest(cx: Cx) {
  const { voice, st } = cx
  stopMicLoopback(cx)
  teardownTestPipeline(cx)

  // If not in voice, reset input level
  if (!st.localAudioStream.value) {
    voice.currentInputLevel = 0
  }
}

