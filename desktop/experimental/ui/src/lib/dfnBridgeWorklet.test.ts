import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import type { DfnFrame, DfnBridgeReport } from './dfnTypes'
import { present } from '../media-test.fixture'

interface TestBridge {
  port: { onmessage: (event: { data: unknown }) => void }
  output: Float32Array
  outputPositions: Float64Array
  totalSamples: number
  nextFrame: number
  readPos: number
  processedFrames: number
  droppedFrames: number
  underrunSamples: number
  workerPort: unknown
  pending: Set<number>
  outputCount: number
  failed: boolean
  receive: (result: unknown) => void
  process: (inputs: Float32Array[][], outputs: Float32Array[][]) => boolean
}
let Processor: (new () => TestBridge) | undefined
let reports: DfnBridgeReport[]
beforeEach(async () => {
  vi.resetModules()
  reports = []
  vi.stubGlobal('AudioWorkletProcessor', class {
    port = { postMessage: (msg: DfnBridgeReport) => reports.push(msg) }
  })
  vi.stubGlobal('registerProcessor', (name: string, constructor: new () => TestBridge) => {
    expect(name).toBe('deepfilter-worker-bridge')
    Processor = constructor
  })
  // Import the production module through Vitest so V8 measures its real code.
  await import('./dfnBridgeWorklet')
})
afterEach(() => vi.unstubAllGlobals())

function createBridge(connect = true) {
  const node = new (present(Processor))()
  const frames: DfnFrame[] = []
  const port = { close() {}, postMessage: (msg: DfnFrame) => frames.push(msg), onmessage: (_event: { data: unknown }) => {} }
  if (connect) node.port.onmessage({ data: { type: 'connect', port } })
  const input = new Float32Array(128).fill(0.25)
  const output = new Float32Array(128)
  const render = () => { node.process([[input]], [[output]]); return output.slice() }
  return { node, frames, reports, render, port }
}

describe('DFN render-thread bridge', () => {
  it('never queues more than three frames behind a stalled worker and bypasses after bounded startup', () => {
    const { node, frames, reports, render } = createBridge()
    let output = new Float32Array()
    for (let i = 0; i < 120; i++) {
      output = render()
      expect(node.pending.size).toBeLessThanOrEqual(3)
      expect(node.outputCount).toBeLessThanOrEqual(1440)
    }
    expect(frames).toHaveLength(3)
    expect(reports).toEqual([{ type: 'failed' }])
    expect(output.every(sample => sample === 0.25)).toBe(true)
  })

  it('recovers filtered playback after a temporary 60ms Worker scheduling gap without increasing buffers', () => {
    const { node, frames, reports, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: present(frames[0]).id, samples: new Float32Array(480).fill(0.1) })
    let delivered = 1
    // Audio keeps rendering while the Worker is temporarily unavailable.
    for (let i = 0; i < 23; i++) {
      render()
      expect(node.pending.size).toBeLessThanOrEqual(3)
      expect(node.outputCount).toBeLessThanOrEqual(1440)
    }
    expect(node.failed).toBe(false)
    // Its outstanding old replies are discarded, then fresh replies resume.
    while (delivered < frames.length) {
      node.receive({ type: 'frame', id: present(frames[delivered++]).id, samples: new Float32Array(480).fill(0.1) })
    }
    let filtered = false
    for (let i = 0; i < 30; i++) {
      filtered ||= render().every(sample => Math.abs(sample - 0.1) < 0.0001)
      while (delivered < frames.length) {
        node.receive({ type: 'frame', id: present(frames[delivered++]).id, samples: new Float32Array(480).fill(0.1) })
      }
      expect(node.pending.size).toBeLessThanOrEqual(3)
      expect(node.outputCount).toBeLessThanOrEqual(1440)
    }
    expect(filtered).toBe(true)
    expect(node.failed).toBe(false)
    expect(reports.some(report => report.type === 'failed')).toBe(false)
  })

  it('plays valid processed output and drops replies older than 30ms', () => {
    const { node, frames, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: present(frames[0]).id, samples: new Float32Array(480).fill(0.1) })
    expect(render().every(sample => Math.abs(sample - 0.1) < 0.0001)).toBe(true)
    node.pending.add(-100)
    const count = node.outputCount
    node.receive({ type: 'frame', id: -100, samples: new Float32Array(480).fill(0.5) })
    expect(node.outputCount).toBe(count)
  })

  it('rejects malformed worker output and reports failure once', () => {
    const { node, frames, reports, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: present(frames[0]).id, samples: new Float32Array(4096) })
    node.receive({ type: 'failed' })
    expect(reports).toEqual([{ type: 'failed' }])
    expect(render().every(sample => sample === 0.25)).toBe(true)
  })
})

describe('DFN bridge protocol and buffering', () => {
  it('ignores invalid commands, duplicate connections and unsolicited replies', () => {
    const { node, port, frames, render } = createBridge(false)
    node.port.onmessage({ data: null })
    node.receive(null)
    render()
    expect(frames).toHaveLength(0)
    node.port.onmessage({ data: { type: 'connect', port } })
    const otherPort = { close() {}, postMessage: vi.fn() }
    node.port.onmessage({ data: { type: 'connect', port: otherPort } })
    for (let i = 0; i < 4; i++) render()
    port.onmessage({ data: { type: 'frame', id: -1, samples: new Float32Array(480) } })
    expect(node.outputCount).toBe(0)
    port.onmessage({ data: { type: 'frame', id: present(frames[0]).id, samples: new Float32Array(480).fill(0.2) } })
    expect(node.outputCount).toBe(480)
    expect(otherPort.postMessage).not.toHaveBeenCalled()
  })

  it('returns alive with absent input or output without changing buffers', () => {
    const { node } = createBridge()
    expect(node.process([], [[]])).toBe(true)
    expect(node.process([[]], [])).toBe(true)
    expect(node.process([[new Float32Array(128)]], [])).toBe(true)
    expect(node.process([[new Float32Array(128)]], [[]])).toBe(true)
    expect(node.totalSamples).toBe(0)
  })

  it('silences bounded startup across every output channel and preserves stereo bypass', () => {
    const { node } = createBridge(false)
    const input = new Float32Array(128).fill(0.7)
    const channels = [new Float32Array(128), new Float32Array(128)]
    expect(node.process([[input]], [channels])).toBe(true)
    expect(channels.every(channel => channel.every(sample => sample === 0))).toBe(true)
    node.port.onmessage({ data: { type: 'failed' } })
    node.process([[input]], [channels])
    expect(channels.every(channel => channel.every(sample => Math.abs(sample - 0.7) < 0.0001))).toBe(true)
    expect(reports).toEqual([{ type: 'failed' }])
  })

  it('rejects a matching reply with a non-PCM payload', () => {
    const { node, frames, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: present(frames[0]).id, samples: [0] })
    expect(node.failed).toBe(true)
    expect(node.pending.size).toBe(0)
    expect(node.outputCount).toBe(0)
  })

  it('drops queued PCM that exceeds the end-to-end 30ms budget', () => {
    const { node, frames, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: present(frames[0]).id, samples: new Float32Array(480).fill(0.1) })
    // Render a longer callback after the worker response has arrived.
    const input = new Float32Array(2000).fill(0.25)
    const output = new Float32Array(2000)
    node.process([[input]], [[output]])
    expect(node.outputCount).toBe(0)
    expect(output.every(sample => sample === 0.25)).toBe(true)
    expect(node.failed).toBe(false)
  })

  it('bounds the output ring under a staged burst, reports numeric diagnostics, and resets recovered underrun', () => {
    const { node } = createBridge()
    // Stage an adversarial receive burst to assert the finite ring invariant.
    // Actual render/worker scheduling and its bounded pending set are exercised
    // above; this directly tests the defensive ring even beyond that schedule.
    for (let id = 0; id < 101; id++) {
      node.nextFrame = id + 1
      node.pending.add(id)
      node.receive({ type: 'frame', id, samples: new Float32Array(480).fill(id / 200) })
      expect(node.outputCount).toBeLessThanOrEqual(1440)
    }
    expect(node.processedFrames).toBe(101)
    expect(reports).toEqual([
      { type: 'stats', processedFrames: 1, droppedFrames: 0, bridgeBudgetMs: 30 },
      { type: 'stats', processedFrames: 100, droppedFrames: 0, bridgeBudgetMs: 30 }
    ])
    node.totalSamples = 480 * 100
    node.underrunSamples = 128
    const output = new Float32Array(128)
    node.process([[new Float32Array(128)]], [[output]])
    expect(node.underrunSamples).toBe(0)
    expect(output.every(sample => Math.abs(sample - 0.49) < 0.0001)).toBe(true)
  })

  it('expires a worker whose earliest pending frame exceeds the stall budget', () => {
    const { node, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.pending.clear()
    node.pending.add(-100)
    for (let i = 0; i < 4; i++) render()
    expect(node.failed).toBe(true)
    expect(reports).toEqual([{ type: 'failed' }])
  })
})

describe('DFN unconnected bridge lifecycle', () => {
  it('expires sustained underrun even when no worker frame is outstanding', () => {
    const { node, render } = createBridge(false)
    let output = new Float32Array()
    for (let i = 0; i < 120; i++) output = render()
    expect(node.pending.size).toBe(0)
    expect(node.failed).toBe(true)
    expect(reports).toEqual([{ type: 'failed' }])
    expect(output.every(sample => sample === 0.25)).toBe(true)
  })
})
