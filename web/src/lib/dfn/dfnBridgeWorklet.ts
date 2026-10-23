import { isDfnConnect, isDfnFailed, isDfnFrameHeader } from './dfnTypes'
/* global AudioWorkletProcessor, registerProcessor */

// The rendering thread only buffers and copies mono PCM. Three 10 ms frames
// bound queued work, and replies older than 30 ms cannot add stale audio.
class DeepFilterBridge extends AudioWorkletProcessor {
  frameLength = 480
  maxFrames = 3
  failureSamples = 480 * 25
  input = new Float32Array(480)
  inputPos = 0
  output = new Float32Array(1440)
  outputPositions = new Float64Array(1440)
  totalSamples = 0
  readPos = 0
  outputCount = 0
  nextFrame = 0
  pending = new Set<number>()
  workerPort: MessagePort | null = null
  failed = false
  hasOutput = false
  underrunSamples = 0
  processedFrames = 0
  droppedFrames = 0

  constructor() {
    super()
    this.frameLength = 480
    this.maxFrames = 3
    this.input = new Float32Array(this.frameLength)
    this.inputPos = 0
    this.output = new Float32Array(this.frameLength * this.maxFrames)
    this.outputPositions = new Float64Array(this.output.length)
    this.totalSamples = 0
    this.readPos = 0
    this.outputCount = 0
    this.nextFrame = 0
    this.pending = new Set<number>()
    this.workerPort = null
    this.failed = false
    this.hasOutput = false
    this.underrunSamples = 0
    this.processedFrames = 0
    this.droppedFrames = 0
    this.port.onmessage = ({ data }: MessageEvent<unknown>) => {
      if (isDfnFailed(data)) { this.fail(data.reason ?? 'closed by the page'); return }
      if (!isDfnConnect(data) || this.workerPort) return
      this.workerPort = data.port
      this.workerPort.onmessage = ({ data: result }: MessageEvent<unknown>) => this.receive(result)
    }
  }

  validateReceivedFrame(result: unknown): { id: number; samples: Float32Array } | null {
    if (isDfnFailed(result)) {
      this.fail(result.reason ?? 'the worker gave up')
      return null
    }
    if (!isDfnFrameHeader(result) || !this.pending.delete(result.id)) return null
    if (!(result.samples instanceof Float32Array) || result.samples.length !== this.frameLength) {
      this.fail('the worker sent a frame of the wrong kind')
      return null
    }
    return { id: result.id, samples: result.samples }
  }

  emitStatsIfNeeded() {
    if (this.processedFrames === 1 || this.processedFrames % 100 === 0) {
      this.port.postMessage({ type: 'stats', processedFrames: this.processedFrames, droppedFrames: this.droppedFrames, bridgeBudgetMs: 30 })
    }
  }

  writeOutputSamples(frameId: number, samples: Float32Array) {
    // Oldest queued samples yield to fresh ones at the finite output limit.
    const excess = Math.max(0, this.outputCount + this.frameLength - this.output.length)
    this.readPos = (this.readPos + excess) % this.output.length
    this.outputCount -= excess
    let writePos = (this.readPos + this.outputCount) % this.output.length
    for (let i = 0; i < samples.length; i++) {
      this.output[writePos] = samples[i] ?? 0
      this.outputPositions[writePos] = frameId * this.frameLength + i + 1
      writePos = (writePos + 1) % this.output.length
    }
    this.outputCount += this.frameLength
    this.hasOutput = true
  }

  receive(result: unknown) {
    const frame = this.validateReceivedFrame(result)
    if (!frame) return
    this.processedFrames++
    const late = this.nextFrame - frame.id > this.maxFrames
    if (late) this.droppedFrames++
    this.emitStatsIfNeeded()
    if (late) return
    this.writeOutputSamples(frame.id, frame.samples)
  }

  // The reason (never audio) tells whoever watches why the filter was given up.
  fail(reason: string) {
    if (this.failed) return
    this.failed = true
    this.pending.clear()
    this.outputCount = 0
    this.port.postMessage({ type: 'failed', reason })
  }

  process(inputs: Float32Array[][], outputs: Float32Array[][]) {
    const input = inputs[0]?.[0]
    const channels = outputs[0]
    if (!input || !channels?.length) return true
    this.totalSamples += input.length
    if (!this.failed) this.collect(input)
    this.dropStaleOutput()
    this.render(input, channels, this.updateBypass(input.length))
    return true
  }

  // Buffers the render quantum into 10 ms frames and hands each full one to the worker.
  collect(input: Float32Array) {
    for (const sample of input) {
      this.input[this.inputPos++] = sample
      if (this.inputPos === this.frameLength) this.submitFrame()
    }
  }

  submitFrame() {
    const id = this.nextFrame++
    const oldest = this.pending.values().next().value
    if (oldest !== undefined && (id - oldest) * this.frameLength > this.failureSamples) this.fail(`the worker took over ${this.failureSamples / 48} ms for a frame`)
    if (!this.failed && this.workerPort && this.pending.size < this.maxFrames) {
      const samples = this.input
      this.pending.add(id)
      this.workerPort.postMessage({ type: 'frame', id, samples }, [samples.buffer])
      this.input = new Float32Array(this.frameLength)
    }
    this.inputPos = 0
  }

  // Include queued output in the 30 ms bridge budget, not only Worker RTT.
  dropStaleOutput() {
    while (this.outputCount > 0 && this.totalSamples - (this.outputPositions[this.readPos] ?? 0) > this.frameLength * this.maxFrames) {
      this.readPos = (this.readPos + 1) % this.output.length
      this.outputCount--
    }
  }

  hasStartedOutput(): boolean {
    return this.hasOutput || this.nextFrame >= this.maxFrames
  }

  // Counts starved samples (failing the bridge after too many) and says whether the dry input is passed through.
  updateBypass(inputLength: number) {
    const underrun = this.outputCount < inputLength
    const activeUnderrun = this.hasStartedOutput() && underrun
    if (activeUnderrun && !this.failed) {
      this.underrunSamples += inputLength
      if (this.underrunSamples > this.failureSamples) this.fail(`no filtered audio for ${this.failureSamples / 48} ms`)
    } else if (!underrun) {
      this.underrunSamples = 0
    }
    return this.failed || activeUnderrun
  }

  render(input: Float32Array, channels: Float32Array[], bypass: boolean) {
    for (let i = 0; i < input.length; i++) {
      const sample = bypass ? input[i] ?? 0 : this.outputCount > 0 ? this.output[this.readPos] ?? 0 : 0
      if (!bypass && this.outputCount > 0) {
        this.readPos = (this.readPos + 1) % this.output.length
        this.outputCount--
      }
      for (const channel of channels) channel[i] = sample
    }
  }
}

registerProcessor('deepfilter-worker-bridge', DeepFilterBridge)
