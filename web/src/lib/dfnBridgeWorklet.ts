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
      if (isDfnFailed(data)) { this.fail(); return }
      if (!isDfnConnect(data) || this.workerPort) return
      this.workerPort = data.port
      this.workerPort.onmessage = ({ data: result }: MessageEvent<unknown>) => this.receive(result)
    }
  }

  receive(result: unknown) {
    if (isDfnFailed(result)) {
      this.fail()
      return
    }
    if (!isDfnFrameHeader(result) || !this.pending.delete(result.id)) return
    if (!(result.samples instanceof Float32Array) || result.samples.length !== this.frameLength) {
      this.fail()
      return
    }
    this.processedFrames++
    const late = this.nextFrame - result.id > this.maxFrames
    if (late) this.droppedFrames++
    // Local numerical diagnostics, first frame then once per second; no PCM.
    if (this.processedFrames === 1 || this.processedFrames % 100 === 0) {
      this.port.postMessage({ type: 'stats', processedFrames: this.processedFrames, droppedFrames: this.droppedFrames, bridgeBudgetMs: 30 })
    }
    if (late) return
    // Oldest queued samples yield to fresh ones at the finite output limit.
    const excess = Math.max(0, this.outputCount + this.frameLength - this.output.length)
    this.readPos = (this.readPos + excess) % this.output.length
    this.outputCount -= excess
    let writePos = (this.readPos + this.outputCount) % this.output.length
    for (let i = 0; i < result.samples.length; i++) {
      this.output[writePos] = result.samples[i] ?? 0
      this.outputPositions[writePos] = result.id * this.frameLength + i + 1
      writePos = (writePos + 1) % this.output.length
    }
    this.outputCount += this.frameLength
    this.hasOutput = true
  }

  fail() {
    if (this.failed) return
    this.failed = true
    this.pending.clear()
    this.outputCount = 0
    this.port.postMessage({ type: 'failed' })
  }

  process(inputs: Float32Array[][], outputs: Float32Array[][]) {
    const input = inputs[0]?.[0]
    const channels = outputs[0]
    if (!input || !channels?.length) return true
    this.totalSamples += input.length
    if (!this.failed) {
      for (const sample of input) {
        this.input[this.inputPos++] = sample
        if (this.inputPos !== this.frameLength) continue
        const id = this.nextFrame++
        const oldest = this.pending.values().next().value
        if (oldest !== undefined && (id - oldest) * this.frameLength > this.failureSamples) this.fail()
        if (!this.failed && this.workerPort && this.pending.size < this.maxFrames) {
          const samples = this.input
          this.pending.add(id)
          this.workerPort.postMessage({ type: 'frame', id, samples }, [samples.buffer])
          this.input = new Float32Array(this.frameLength)
        }
        this.inputPos = 0
      }
    }
    // Include queued output in the 30 ms bridge budget, not only Worker RTT.
    while (this.outputCount > 0 && this.totalSamples - (this.outputPositions[this.readPos] ?? 0) > this.frameLength * this.maxFrames) {
      this.readPos = (this.readPos + 1) % this.output.length
      this.outputCount--
    }
    const underrun = this.outputCount < input.length
    const startupExpired = this.nextFrame >= this.maxFrames
    if ((this.hasOutput || startupExpired) && underrun && !this.failed) {
      this.underrunSamples += input.length
      if (this.underrunSamples > this.failureSamples) this.fail()
    } else if (!underrun) this.underrunSamples = 0
    const bypass = this.failed || ((this.hasOutput || startupExpired) && underrun)
    for (let i = 0; i < input.length; i++) {
      const sample = bypass ? input[i] ?? 0 : this.outputCount > 0 ? this.output[this.readPos] ?? 0 : 0
      if (!bypass && this.outputCount > 0) {
        this.readPos = (this.readPos + 1) % this.output.length
        this.outputCount--
      }
      for (const channel of channels) channel[i] = sample
    }
    return true
  }
}

registerProcessor('deepfilter-worker-bridge', DeepFilterBridge)
