import { describe, it, expect } from 'vitest'
import { runInNewContext } from 'node:vm'
import source from './dfnBridgeWorklet.js?raw'

function createBridge() {
  let Processor
  const reports = []
  runInNewContext(source, {
    Float32Array,
    AudioWorkletProcessor: class { constructor() { this.port = { postMessage: msg => reports.push(msg) } } },
    registerProcessor: (_name, processor) => { Processor = processor }
  })
  const node = new Processor()
  const frames = []
  node.port.onmessage({ data: { type: 'connect', port: { postMessage: msg => frames.push(msg) } } })
  const input = new Float32Array(128).fill(0.25)
  const output = new Float32Array(128)
  const render = () => { node.process([[input]], [[output]]); return output.slice() }
  return { node, frames, reports, render }
}

describe('DFN render-thread bridge', () => {
  it('never queues more than three frames behind a stalled worker and bypasses after bounded startup', () => {
    const { node, frames, reports, render } = createBridge()
    let output
    for (let i = 0; i < 100; i++) {
      output = render()
      expect(node.pending.size).toBeLessThanOrEqual(3)
      expect(node.outputCount).toBeLessThanOrEqual(1440)
    }
    expect(frames).toHaveLength(3)
    expect(reports).toEqual([{ type: 'failed' }])
    expect(output.every(sample => sample === 0.25)).toBe(true)
  })

  it('plays valid processed output and drops replies older than 30ms', () => {
    const { node, frames, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: frames[0].id, samples: new Float32Array(480).fill(0.1) })
    expect(render().every(sample => Math.abs(sample - 0.1) < 0.0001)).toBe(true)
    node.pending.add(-100)
    const count = node.outputCount
    node.receive({ type: 'frame', id: -100, samples: new Float32Array(480).fill(0.5) })
    expect(node.outputCount).toBe(count)
  })

  it('rejects malformed worker output and reports failure once', () => {
    const { node, frames, reports, render } = createBridge()
    for (let i = 0; i < 4; i++) render()
    node.receive({ type: 'frame', id: frames[0].id, samples: new Float32Array(4096) })
    node.receive({ type: 'failed' })
    expect(reports).toEqual([{ type: 'failed' }])
    expect(render().every(sample => sample === 0.25)).toBe(true)
  })
})
