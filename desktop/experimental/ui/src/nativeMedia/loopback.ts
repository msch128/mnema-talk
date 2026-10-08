import { NativeMediaSocket, type MediaSocket } from './native-media-socket'
import type { NativeMediaBridge } from './native-media-channel'
export interface HookCount { nativeEmptyFrames: number; status: string; frames: number; bytes: number; drops: number; rejectionCounts: Record<string, number>; rejectionInputLengthClasses: { zero: number; nonzero: number } }
export interface Report {
  run: number
  fixtureMode: string
  signaling: Record<string, string>
  authenticatedAudioAtFault: number
  serverPeersAfterCutoff: number
  injectedTamper: number
  flowBeforeFault: boolean
  faultRequested: boolean
  fixtureRevokeAcknowledged: boolean
  framesAtTransportClose: number
  cutoffStableSeconds: number
  cutoffBaselineEstablished: boolean
  framesDuringCutoffDrain: number
  framesAtCutoff: number
  framesAfterCutoff: number
  passedFaultScenario: boolean
  mode: string
  phase: string
  seconds: number
  senderState: string
  receiverState: string
  sourceAudioState: string
  displayCounterSource: string
  decodedFrames: number
  videoTime: number
  videoWidth: number
  audioRms: number
  peakAudioRms: number
  outboundVideoPackets: number
  outboundAudioPackets: number
  inboundVideoPackets: number
  inboundAudioPackets: number
  inboundFramesDecoded: number
  inboundAudioEnergy: number
  codecNames: string[]
  negotiatedCodecs: Record<string, string>
  hooks: Record<string, HookCount>
  receiverBindings: { kind: string; mid: string | null; assignment: string; retained: boolean }[]
  playbackAttempts: number
  playbackSettled: string
  protectedMedia: boolean
  sframeSelftests: Record<string, string[]>
  allTracksEnded: boolean
  workerTerminated: boolean
  passedSyntheticFlow: boolean
  errors: string[]
}

export function emptyReport(run: number, withTransforms: boolean): Report {
  return {
    run, fixtureMode: 'forward', signaling: {}, authenticatedAudioAtFault: 0, serverPeersAfterCutoff: -1, injectedTamper: 0, flowBeforeFault: false, faultRequested: false, fixtureRevokeAcknowledged: false, framesAtTransportClose: 0, cutoffStableSeconds: 0, cutoffBaselineEstablished: false, framesDuringCutoffDrain: 0, framesAtCutoff: 0, framesAfterCutoff: 0, passedFaultScenario: false, mode: withTransforms ? 'encoded worker pass-through' : 'baseline without transform', phase: 'starting', seconds: 0,
    senderState: 'new', receiverState: 'new', sourceAudioState: 'not-started', displayCounterSource: 'unavailable', decodedFrames: 0, videoTime: 0, videoWidth: 0,
    audioRms: 0, peakAudioRms: 0, outboundVideoPackets: 0, outboundAudioPackets: 0, inboundVideoPackets: 0, inboundAudioPackets: 0,
    inboundFramesDecoded: 0, inboundAudioEnergy: 0, codecNames: [], negotiatedCodecs: {}, hooks: {}, receiverBindings: [], playbackAttempts: 0, playbackSettled: 'not-requested', protectedMedia: false, sframeSelftests: {}, allTracksEnded: false,
    workerTerminated: false, passedSyntheticFlow: false, errors: []
  }
}

export class SyntheticLoopback {
  readonly report: Report
  private sockets = new Map<string, MediaSocket>()
  private cancelSignaling = new Set<() => void>()
  private cutoffStarted = 0
  private cutoffTimer: ReturnType<typeof setTimeout> | undefined
  private faultTimer: ReturnType<typeof setTimeout> | undefined
  private sender: RTCPeerConnection | undefined
  private receiver: RTCPeerConnection | undefined
  private audio: AudioContext | undefined
  private oscillator: OscillatorNode | undefined
  private analyser: AnalyserNode | undefined
  private workers = new Map<string, Worker>()
  private receiverHooks = new Map<RTCRtpReceiver, { transform: RTCRtpScriptTransform; assignment: string }>()
  private baseKey: Uint8Array | undefined
  private tracks: MediaStreamTrack[] = []
  private drawing: ReturnType<typeof setInterval> | undefined
  private sampling: ReturnType<typeof setInterval> | undefined
  private expiry: ReturnType<typeof setTimeout> | undefined
  private videoCallback: number | undefined
  private active = true
  private setupStarted = false
  private stopping: Promise<void> | undefined
  private samplingBusy = false
  private started = performance.now()
  private receivedVideo = new MediaStream()
  private videoFrames = 0
  private videoPacketsAtFirstSample = 0
  private audioPacketsAtFirstSample = 0

  constructor(
    private canvas: HTMLCanvasElement,
    private video: HTMLVideoElement,
    private withTransforms: boolean,
    run: number,
    private emit: (report: Report) => void,
    private withSframe = true,
    private fixtureMode: 'forward' | 'tamper' | 'revoke' | 'close' = 'forward',
    private nativeBridge?: NativeMediaBridge
  ) {
    this.report = emptyReport(run, withTransforms)
    this.report.fixtureMode = fixtureMode
    this.report.protectedMedia = withSframe
    if (withSframe) this.report.mode = 'Native authenticated Pion SFU qualification + synthetic RFC9605 VP8/Opus; ephemeral fixture key, no MLS grant'
  }

  private displayedFrames(): number {
    if (typeof this.video.requestVideoFrameCallback === 'function') {
      this.report.displayCounterSource = 'videoFrameCallback'
      return this.videoFrames
    }
    if (typeof this.video.getVideoPlaybackQuality === 'function') {
      this.report.displayCounterSource = 'videoPlaybackQuality'
      return this.video.getVideoPlaybackQuality().totalVideoFrames
    }
    this.report.displayCounterSource = 'unavailable'
    return 0
  }

  private publish() {
    this.report.seconds = Math.round((performance.now() - this.started) / 100) / 10
    this.emit(structuredClone(this.report))
  }

  private fail(code: string) {
    if (!this.active) return
    this.report.passedSyntheticFlow = false
    this.report.passedFaultScenario = false
    if (!this.report.errors.includes(code)) this.report.errors.push(code)
    this.publish()
  }

  private hook(endpoint: RTCRtpSender | RTCRtpReceiver, id: string) {
    if (!this.withTransforms) return
    const worker = this.workers.get(id.startsWith('send-') ? 'send' : 'receive')
    if (!worker) throw new Error('transform_worker_missing')
    endpoint.transform = new RTCRtpScriptTransform(worker, { id, sframe: this.withSframe, key: this.baseKey, context: id.endsWith('video') ? 1 : 2 })
    this.report.hooks[id] = { nativeEmptyFrames: 0, status: 'waiting', frames: 0, bytes: 0, drops: 0, rejectionCounts: {}, rejectionInputLengthClasses: { zero: 0, nonzero: 0 } }
  }

  private hookReceiver(endpoint: RTCRtpReceiver, assignment: string) {
    if (!this.withTransforms || this.receiverHooks.has(endpoint)) return
    this.hook(endpoint, `receive-${endpoint.track.kind}`)
    if (!endpoint.transform) throw new Error('receiver_transform_assignment_failed')
    this.receiverHooks.set(endpoint, { transform: endpoint.transform, assignment })
  }

  private bindingDiagnostics() {
    this.report.receiverBindings = this.receiver?.getTransceivers().filter(transceiver => this.receiverHooks.has(transceiver.receiver)).map(transceiver => {
      const binding = this.receiverHooks.get(transceiver.receiver)
      return { kind: transceiver.receiver.track.kind, mid: transceiver.mid, assignment: binding?.assignment ?? 'baseline', retained: !this.withTransforms || binding?.transform === transceiver.receiver.transform }
    }) ?? []
    if (this.withTransforms && this.report.receiverBindings.some(binding => !binding.retained)) this.fail('receiver_transform_binding_lost')
  }

  async start(): Promise<void> {
    if (!this.active || this.setupStarted) return
    this.setupStarted = true
    if (!this.nativeBridge || !this.withTransforms || !this.withSframe) { this.fail('native_protected_media_qualification_required'); await this.stop(); return }
    try {
      if (this.withTransforms && typeof RTCRtpScriptTransform !== 'function') {
        this.fail('encoded_transform_api_unavailable')
        await this.stop()
        return
      }
      const context = this.canvas.getContext('2d')
      if (!context || typeof this.canvas.captureStream !== 'function') throw new Error('synthetic_canvas_unavailable')
      this.canvas.width = 320
      this.canvas.height = 180
      let frame = 0
      const draw = () => {
        frame++
        context.fillStyle = '#0F1110'
        context.fillRect(0, 0, 320, 180)
        context.fillStyle = '#2DA771'
        context.fillRect((frame * 4) % 280, 20, 40, 120)
        context.fillStyle = '#E6EAE8'
        context.font = '18px sans-serif'
        context.fillText(`Synthetic frame ${frame}`, 12, 165)
      }
      draw()
      this.drawing = setInterval(draw, 50)
      const source = this.canvas.captureStream(20)
      this.tracks.push(...source.getTracks())
      this.audio = new AudioContext()
      await this.audio.resume()
      if (!this.active) return
      const audioDestination = this.audio.createMediaStreamDestination()
      this.oscillator = this.audio.createOscillator()
      this.oscillator.frequency.value = 440
      const gain = this.audio.createGain()
      gain.gain.value = 0.15
      this.oscillator.connect(gain).connect(audioDestination)
      this.oscillator.start()
      for (const track of audioDestination.stream.getTracks()) {
        source.addTrack(track)
        this.tracks.push(track)
      }
      this.report.sourceAudioState = this.audio.state
      if (this.withSframe) this.baseKey = crypto.getRandomValues(new Uint8Array(32))

      if (this.withTransforms) {
        for (const direction of ['send', 'receive']) {
        const worker = new Worker(new URL('./transform.worker.ts', import.meta.url), { type: 'module', name: `worker-${direction}` })
        this.workers.set(direction, worker)
        worker.onmessage = event => {
          if (!this.active) return
          const data = event.data as { id: string; status: string; frames: number; bytes: number; drops: number; tamperInjected?: number; rejectionCounts?: Record<string, number>; nativeEmptyFrames?: number; rejectionInputLengthClasses?: { zero: number; nonzero: number }; selftests?: string[] }
          if (typeof data.id !== 'string' || typeof data.frames !== 'number' || typeof data.bytes !== 'number') return
          this.report.hooks[data.id] = { nativeEmptyFrames: data.nativeEmptyFrames ?? 0, status: data.status, frames: data.frames, bytes: data.bytes, drops: data.drops, rejectionCounts: data.rejectionCounts ?? {}, rejectionInputLengthClasses: data.rejectionInputLengthClasses ?? { zero: 0, nonzero: 0 } }
          if (data.tamperInjected) this.report.injectedTamper = data.tamperInjected
          if (data.selftests) this.report.sframeSelftests[data.id] = data.selftests
          if (data.status === 'pipeline_error') this.fail('encoded_transform_pipeline_error')
          if (data.status === 'protected_frame_rejected' || data.status === 'protected_setup_rejected') this.fail('sframe_protected_path_rejected')
          this.publish()
        }
        worker.onerror = event => { event.preventDefault(); this.fail(`encoded_transform_${direction}_worker_error`) }
        }
      }

      this.sender = new RTCPeerConnection({ iceServers: [] })
      this.receiver = new RTCPeerConnection({ iceServers: [] })
      const sender = this.sender
      const receiver = this.receiver
      sender.onconnectionstatechange = () => { if (!this.active) return; this.report.senderState = sender.connectionState; this.publish() }
      receiver.onconnectionstatechange = () => { if (!this.active) return; this.report.receiverState = receiver.connectionState; this.publish() }
      this.video.muted = true
      this.video.defaultMuted = true
      this.video.playsInline = true
      this.video.srcObject = this.receivedVideo
      receiver.ontrack = event => {
        if (!this.active) { event.track.stop(); return }
        // Bind the receiver created for the offered m-section, rather than an
        // unassociated pre-created recvonly transceiver that may remain unused.
        this.hookReceiver(event.receiver, 'ontrack-before-answer')
        this.tracks.push(event.track)
        if (event.track.kind === 'audio' && this.audio) {
          this.analyser = this.audio.createAnalyser()
          this.analyser.fftSize = 2048
          const silentOutput = this.audio.createGain()
          silentOutput.gain.value = 0
          this.audio.createMediaStreamSource(new MediaStream([event.track])).connect(this.analyser).connect(silentOutput).connect(this.audio.destination)
        }
        if (event.track.kind === 'video') {
          this.receivedVideo.addTrack(event.track)
          this.report.playbackAttempts++
          this.report.playbackSettled = 'pending'
          void this.video.play().then(() => {
            if (this.active) { this.report.playbackSettled = 'resolved'; this.publish() }
          }, error => {
            if (!this.active) return
            const knownNames = ['AbortError', 'NotAllowedError', 'NotSupportedError', 'InvalidStateError', 'SecurityError']
            const name = error instanceof DOMException && knownNames.includes(error.name) ? error.name : 'UnknownError'
            this.report.playbackSettled = name
            this.fail(`received_video_playback_rejected_${name}`)
          })
        }
      }
      const countFrame = () => {
        if (!this.active) return
        this.videoFrames++
        this.videoCallback = this.video.requestVideoFrameCallback(countFrame)
      }
      if (typeof this.video.requestVideoFrameCallback === 'function') this.videoCallback = this.video.requestVideoFrameCallback(countFrame)
      this.report.phase = 'negotiating-two-clients-through-real-sfu'
      this.publish()
      await Promise.all([this.connectPeer('publisher', sender, source), this.connectPeer('viewer', receiver, source)])
      if (!this.active) return
      this.report.phase = 'measuring-12-seconds'
      this.sampling = setInterval(() => { void this.sample() }, 500)
      this.expiry = setTimeout(() => { void this.stop() }, 12000)
      await this.sample()
    } catch (error) {
      const known = error instanceof Error && ['transform_worker_missing', 'synthetic_canvas_unavailable', 'sframe_supported_codec_unavailable'].includes(error.message)
      this.fail(known && error instanceof Error ? error.message : 'native_synthetic_media_setup_failed')
      await this.stop()
    }
  }

  private connectPeer(role: 'publisher' | 'viewer', peer: RTCPeerConnection, source: MediaStream): Promise<void> {
    return new Promise((resolve, reject) => {
      const cancel = () => reject(new Error('fixture_stopped_before_answer'))
      this.cancelSignaling.add(cancel)
      const socket = this.nativeBridge ? new NativeMediaSocket(this.nativeBridge, role) : (() => { throw new Error('native_media_actor_unavailable') })()
      this.sockets.set(role, socket)
      const candidates: RTCIceCandidateInit[] = []
      let queue = Promise.resolve()
      let answered = false
      const send = (type: string, payload?: unknown) => {
        if (!this.active || socket.readyState !== 1) return
        socket.send(JSON.stringify({ type, ...(payload === undefined ? {} : { payload }) }))
      }
      peer.onicecandidate = event => { if (event.candidate) send('ice', event.candidate.toJSON()) }
      socket.onopen = () => { if (!this.active) { socket.close(); return }; this.report.signaling[role] = 'open'; this.publish() }
      socket.onerror = () => { if (this.active) this.fail('fixture_local_signaling_error'); reject(new Error('fixture_local_signaling_error')) }
      socket.onclose = () => {
        if (!this.active) return
        this.report.signaling[role] = 'closed'
        const expectedClose = this.report.faultRequested && (this.fixtureMode === 'revoke' && role === 'viewer' || this.fixtureMode === 'close' && role === 'publisher')
        if (!expectedClose) { this.fail('fixture_unexpected_transport_close'); void this.stop() }
        else {
          this.report.framesAtTransportClose = this.displayedFrames()
          peer.close()
          this.report.passedSyntheticFlow = false
          if (role === 'viewer') {
            for (const endpoint of peer.getReceivers()) endpoint.track.stop()
            this.video.pause()
            const worker = this.workers.get('receive'); worker?.terminate()
            this.report.framesAtCutoff = this.displayedFrames()
            this.report.cutoffBaselineEstablished = true
            this.cutoffStarted = performance.now()
          } else {
            for (const track of source.getTracks()) track.stop()
            clearInterval(this.drawing)
            try { this.oscillator?.stop() } catch { /* Already ended. */ }
            this.workers.get('send')?.terminate()
            // Report buffered playback separately; after a fixed one-second drain,
            // require a stable display count. This is not server revocation grace.
            this.cutoffTimer = setTimeout(() => {
              if (!this.active) return
              this.report.framesDuringCutoffDrain = this.displayedFrames() - this.report.framesAtTransportClose
              this.report.framesAtCutoff = this.displayedFrames()
              this.report.cutoffBaselineEstablished = true
              this.cutoffStarted = performance.now()
            }, 1000)
          }
          this.publish()
        }
        if (!answered) reject(new Error('fixture_local_signaling_closed_before_answer'))
      }
      socket.onmessage = event => {
        if (!this.active) return
        queue = queue.then(async () => {
          if (!this.active) return
          const message = JSON.parse(String(event.data)) as { type: string; payload?: RTCSessionDescriptionInit | RTCIceCandidateInit; media_peers?: number }
          if (message.type === 'viewer-revoked') { this.report.fixtureRevokeAcknowledged = true; this.report.serverPeersAfterCutoff = message.media_peers ?? -1; this.publish(); return }
          if (message.type === 'joined') return
          if (message.type === 'ice') {
            if (!peer.remoteDescription) { if (candidates.length >= 64) throw new Error('fixture_candidate_limit'); candidates.push(message.payload as RTCIceCandidateInit) }
            else { await peer.addIceCandidate(message.payload as RTCIceCandidateInit); if (!this.active) return }
            return
          }
          if (message.type !== 'offer') throw new Error('fixture_protocol_rejected')
          await peer.setRemoteDescription(message.payload as RTCSessionDescriptionInit)
          if (!this.active) return
          const transceivers = peer.getTransceivers()
          if (role === 'publisher' && !answered) {
            for (const track of source.getTracks()) {
              const transmit = transceivers.find(item => item.receiver.track.kind === track.kind)
              if (!transmit) throw new Error('fixture_source_transceiver_missing')
              await transmit.sender.replaceTrack(track)
              if (!this.active) return
              transmit.direction = 'sendonly'
              this.hook(transmit.sender, `send-${track.kind}`)
            }
          }
          for (const transceiver of transceivers) {
            if (role === 'viewer') transceiver.direction = 'recvonly'
            const wanted = transceiver.receiver.track.kind === 'video' ? 'video/vp8' : 'audio/opus'
            const codecs = RTCRtpSender.getCapabilities(transceiver.receiver.track.kind)?.codecs.filter(codec => codec.mimeType.toLowerCase() === wanted) ?? []
            if (!codecs.length || typeof transceiver.setCodecPreferences !== 'function') throw new Error('sframe_supported_codec_unavailable')
            transceiver.setCodecPreferences(codecs)
          }
          for (const candidate of candidates.splice(0)) { await peer.addIceCandidate(candidate); if (!this.active) return }
          const answer = await peer.createAnswer()
          if (!this.active) return
          await peer.setLocalDescription(answer)
          if (!this.active) return
          send('answer', peer.localDescription)
          answered = true
          if (['send-video', 'send-audio', 'receive-video', 'receive-audio'].every(id => !!this.report.hooks[id])) { this.baseKey?.fill(0); this.baseKey = undefined }
          this.bindingDiagnostics()
          this.cancelSignaling.delete(cancel)
          resolve()
        }).catch(() => { this.fail('fixture_native_signaling_failed'); reject(new Error('fixture_native_signaling_failed')) })
      }
    })
  }

  private requestFault() {
    if (this.fixtureMode === 'forward' || this.report.faultRequested || !this.report.passedSyntheticFlow) return
    this.report.authenticatedAudioAtFault = this.report.hooks['receive-audio']?.frames ?? 0
    this.report.flowBeforeFault = true
    this.report.faultRequested = true
    if (this.fixtureMode === 'tamper') this.workers.get('send')?.postMessage({ type: 'tamper-next', id: 'send-audio' })
    else if (this.fixtureMode === 'revoke') { this.fail('native_fixture_revocation_action_unavailable'); void this.stop() }
    else { this.report.framesAtCutoff = this.displayedFrames(); this.sockets.get('publisher')?.close() }
    this.publish()
  }

  private async sample() {
    if (!this.active || this.samplingBusy || !this.sender || !this.receiver) return
    this.samplingBusy = true
    try {
      const [transmit, receive] = await Promise.all([this.sender.connectionState === 'closed' ? Promise.resolve(new Map<string, Record<string, unknown>>()) : this.sender.getStats(), this.receiver.connectionState === 'closed' ? Promise.resolve(new Map<string, Record<string, unknown>>()) : this.receiver.getStats()])
      if (!this.active) return
      const numeric = (entry: Record<string, unknown>, key: string) => typeof entry[key] === 'number' ? entry[key] as number : 0
      const codecs = new Set<string>()
      transmit.forEach((entry: Record<string, unknown>) => {
        if (entry.type === 'codec' && typeof entry.mimeType === 'string') codecs.add(entry.mimeType)
        if (entry.type === 'outbound-rtp') {
          const kind = entry.kind ?? entry.mediaType
          const codec = typeof entry.codecId === 'string' ? transmit.get(entry.codecId) : undefined
          if ((kind === 'video' || kind === 'audio') && typeof codec?.mimeType === 'string') this.report.negotiatedCodecs[`send-${kind}`] = codec.mimeType.toLowerCase()
          if (entry.kind === 'video' || entry.mediaType === 'video') this.report.outboundVideoPackets = numeric(entry, 'packetsSent')
          if (entry.kind === 'audio' || entry.mediaType === 'audio') this.report.outboundAudioPackets = numeric(entry, 'packetsSent')
        }
      })
      receive.forEach((entry: Record<string, unknown>) => {
        if (entry.type === 'codec' && typeof entry.mimeType === 'string') codecs.add(entry.mimeType)
        if (entry.type === 'inbound-rtp') {
          const kind = entry.kind ?? entry.mediaType
          const codec = typeof entry.codecId === 'string' ? receive.get(entry.codecId) : undefined
          if ((kind === 'video' || kind === 'audio') && typeof codec?.mimeType === 'string') this.report.negotiatedCodecs[`receive-${kind}`] = codec.mimeType.toLowerCase()
          if (entry.kind === 'video' || entry.mediaType === 'video') {
            this.report.inboundVideoPackets = numeric(entry, 'packetsReceived')
            this.report.inboundFramesDecoded = numeric(entry, 'framesDecoded')
          }
          if (entry.kind === 'audio' || entry.mediaType === 'audio') {
            this.report.inboundAudioPackets = numeric(entry, 'packetsReceived')
            this.report.inboundAudioEnergy = numeric(entry, 'totalAudioEnergy')
          }
        }
      })
      this.report.codecNames = [...codecs].sort()
      if (this.withSframe && Object.entries(this.report.negotiatedCodecs).some(([id, codec]) => codec !== (id.endsWith('video') ? 'video/vp8' : 'audio/opus'))) this.fail('sframe_negotiated_codec_mismatch')
      this.bindingDiagnostics()
      this.report.decodedFrames = this.displayedFrames()
      this.report.videoTime = Math.round(this.video.currentTime * 1000) / 1000
      this.report.videoWidth = this.video.videoWidth
      if (this.analyser) {
        const samples = new Float32Array(this.analyser.fftSize)
        this.analyser.getFloatTimeDomainData(samples)
        const rms = Math.sqrt(samples.reduce((sum, value) => sum + value * value, 0) / samples.length)
        this.report.audioRms = Math.round(rms * 100000) / 100000
        this.report.peakAudioRms = Math.max(this.report.audioRms, this.report.peakAudioRms)
      }
      if (this.report.seconds < 2) {
        this.videoPacketsAtFirstSample = this.report.inboundVideoPackets
        this.audioPacketsAtFirstSample = this.report.inboundAudioPackets
      }
      const hookFlow = !this.withTransforms || ['send-video', 'receive-video', 'send-audio', 'receive-audio'].every(id => (this.report.hooks[id]?.frames ?? 0) > 20)
      const protectedFlow = !this.withSframe || ['send-video', 'send-audio'].every(id => this.report.hooks[id]?.status === 'encrypted')
        && ['receive-video', 'receive-audio'].every(id => this.report.hooks[id]?.status === 'authenticated')
        && this.report.receiverBindings.length === 2 && this.report.receiverBindings.every(binding => binding.retained)
        && ['worker-send', 'worker-receive'].every(id => this.report.sframeSelftests[id]?.length === 25)
        && ['send-video', 'receive-video'].every(id => this.report.negotiatedCodecs[id] === 'video/vp8')
        && ['send-audio', 'receive-audio'].every(id => this.report.negotiatedCodecs[id] === 'audio/opus')
        && Object.values(this.report.hooks).every(hook => hook.drops === 0)
      this.report.passedSyntheticFlow = this.report.errors.length === 0 && this.report.senderState === 'connected' && this.report.receiverState === 'connected'
        && this.report.inboundVideoPackets > this.videoPacketsAtFirstSample + 10 && this.report.inboundAudioPackets > this.audioPacketsAtFirstSample + 20
        && this.report.decodedFrames > 20 && this.report.videoWidth === 320 && this.report.videoTime > 2 && this.report.peakAudioRms > 0.01 && hookFlow && protectedFlow
      if (this.report.passedSyntheticFlow && !this.report.flowBeforeFault && this.fixtureMode !== 'forward' && !this.faultTimer) this.faultTimer = setTimeout(() => { this.requestFault() }, 1000)
      if (this.report.faultRequested) {
        this.report.cutoffStableSeconds = this.cutoffStarted ? Math.floor((performance.now() - this.cutoffStarted) / 100) / 10 : 0
        this.report.framesAfterCutoff = this.report.cutoffBaselineEstablished ? this.displayedFrames() - this.report.framesAtCutoff : 0
        if (this.fixtureMode === 'tamper') this.report.passedFaultScenario = this.report.flowBeforeFault && this.report.injectedTamper === 1 && (this.report.hooks['receive-audio']?.frames ?? 0) > this.report.authenticatedAudioAtFault + 20 && Object.values(this.report.hooks).reduce((count, hook) => count + hook.drops, 0) === 1 && (this.report.hooks['receive-audio']?.rejectionCounts.authentication_failed ?? 0) === 1 && this.report.errors.every(code => code === 'sframe_protected_path_rejected')
        else this.report.passedFaultScenario = this.report.flowBeforeFault && this.report.cutoffBaselineEstablished && this.report.cutoffStableSeconds >= 3 && this.report.signaling[this.fixtureMode === 'revoke' ? 'viewer' : 'publisher'] === 'closed' && this.report.framesAfterCutoff === 0 && (this.fixtureMode !== 'revoke' || this.report.fixtureRevokeAcknowledged && this.report.serverPeersAfterCutoff === 1) && this.report.errors.length === 0
      }
      this.publish()
    } catch { this.fail('native_stats_sample_failed') }
    finally { this.samplingBusy = false }
  }

  stop(): Promise<void> {
    if (this.stopping) return this.stopping
    if (!this.active) return Promise.resolve()
    this.active = false
    this.stopping = this.teardown()
    return this.stopping
  }

  private teardownFailure(code: string) {
    this.report.passedSyntheticFlow = false
    this.report.passedFaultScenario = false
    if (!this.report.errors.includes(code)) this.report.errors.push(code)
  }

  private async teardown(): Promise<void> {
    this.baseKey?.fill(0)
    this.baseKey = undefined
    clearInterval(this.drawing)
    clearInterval(this.sampling)
    clearTimeout(this.expiry)
    clearTimeout(this.faultTimer)
    clearTimeout(this.cutoffTimer)
    for (const cancel of this.cancelSignaling) cancel()
    this.cancelSignaling.clear()
    for (const socket of this.sockets.values()) { socket.onopen = null; socket.onmessage = null; socket.onclose = null; socket.onerror = null }
    const closingSockets = Promise.allSettled([...this.sockets.values()].map(socket => socket.close()))
    if (this.videoCallback !== undefined) this.video.cancelVideoFrameCallback(this.videoCallback)
    for (const peer of [this.sender, this.receiver]) {
      if (!peer) continue
      peer.onicecandidate = null
      peer.onconnectionstatechange = null
      peer.ontrack = null
      peer.close()
    }
    for (const track of this.tracks) track.stop()
    try { this.oscillator?.stop() } catch { /* May already have stopped during teardown. */ }
    for (const worker of this.workers.values()) worker.terminate()
    const closingAudio = this.audio && this.audio.state !== 'closed' ? this.audio.close().catch(() => { this.teardownFailure('audio_context_close_failed') }) : Promise.resolve()
    const socketResults = await closingSockets
    if (socketResults.some(result => result.status === 'rejected')) this.teardownFailure('native_media_actor_close_failed')
    await closingAudio
    this.video.pause()
    this.video.srcObject = null
    this.report.phase = 'stopped'
    this.report.senderState = this.sender?.connectionState ?? 'not-created'
    this.report.receiverState = this.receiver?.connectionState ?? 'not-created'
    this.report.sourceAudioState = this.audio?.state ?? 'not-created'
    this.report.allTracksEnded = this.tracks.every(track => track.readyState === 'ended')
    this.report.workerTerminated = this.workers.size === 2
    this.publish()
  }
}
