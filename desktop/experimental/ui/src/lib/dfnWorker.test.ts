import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DfnFrame, DfnFrameReply, DfnWorkerReply } from './dfnTypes'
import { present } from '../media-test.fixture'

const glue = vi.hoisted(() => ({
  initSync: vi.fn(), df_create: vi.fn(), df_get_frame_length: vi.fn(), df_process_frame: vi.fn()
}))
vi.mock('../third_party/deepfilternet3/worker-glue.js', () => glue)
let worker: { onmessage: (event: { data: unknown }) => void; postMessage: ReturnType<typeof vi.fn> }
function send(data: unknown) { worker.onmessage({ data }) }
const module = new WebAssembly.Module(new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0]))
const init = { type: 'init', wasmModule: module, modelBytes: new ArrayBuffer(8), suppressionLevel: 100 }
function port() { return { onmessage: (_event: { data: unknown }) => {}, close: vi.fn(), postMessage: vi.fn<(data: DfnFrameReply, transfers?: Transferable[]) => void>() } }

beforeEach(async () => {
  vi.resetModules()
  vi.clearAllMocks()
  glue.initSync.mockReturnValue({})
  glue.df_create.mockReturnValue(42)
  glue.df_get_frame_length.mockReturnValue(480)
  glue.df_process_frame.mockImplementation((_handle: number, samples: Float32Array) => Float32Array.from(samples, sample => sample / 2))
  worker = { onmessage: () => {}, postMessage: vi.fn<(data: DfnWorkerReply) => void>() }
  vi.stubGlobal('self', worker)
  // The owned worker module executes directly; only third-party WASM glue is mocked.
  await import('./dfnWorker')
})
afterEach(() => vi.unstubAllGlobals())

describe('DFN inference worker', () => {
  it('initializes once, requires readiness before connecting and ignores duplicate connections', () => {
    const early = port()
    send(null)
    send({ type: 'connect', port: early })
    send(init)
    send(init)
    expect(glue.initSync).toHaveBeenCalledOnce()
    expect(glue.initSync).toHaveBeenCalledWith({ module })
    expect(glue.df_create).toHaveBeenCalledWith(new Uint8Array(init.modelBytes), 100)
    expect(worker.postMessage).toHaveBeenCalledExactlyOnceWith({ type: 'ready', frameLength: 480 })
    const active = port()
    send({ type: 'connect', port: active })
    send({ type: 'connect', port: early })
    const samples = new Float32Array(480).fill(0.5)
    early.onmessage({ data: { type: 'frame', id: 0, samples } })
    expect(glue.df_process_frame).not.toHaveBeenCalled()
    active.onmessage({ data: { type: 'frame', id: 0, samples } })
    expect(glue.df_process_frame).toHaveBeenCalledWith(42, samples)
    const reply = present(active.postMessage.mock.calls[0])
    expect(reply[0]).toMatchObject({ type: 'frame', id: 0 })
    if (reply[0].type !== 'frame') throw new Error('Expected processed PCM')
    expect(reply[0].samples.every(sample => sample === 0.25)).toBe(true)
    expect(reply[1]).toEqual([reply[0].samples.buffer])
  })

  it('drops malformed frames without running inference', () => {
    send(init)
    const active = port()
    send({ type: 'connect', port: active })
    for (const data of [null, {}, { type: 'frame', id: 1, samples: [] }, { type: 'frame', id: 2, samples: new Float32Array(128) }]) active.onmessage({ data })
    expect(glue.df_process_frame).not.toHaveBeenCalled()
    expect(active.postMessage).not.toHaveBeenCalled()
  })

  it.each(['init', 'create', 'frame-length'] as const)('reports %s setup failure', stage => {
    if (stage === 'init') glue.initSync.mockImplementationOnce(() => { throw new Error('bad module') })
    if (stage === 'create') glue.df_create.mockImplementationOnce(() => { throw new Error('bad model') })
    if (stage === 'frame-length') glue.df_get_frame_length.mockReturnValueOnce(256)
    send(init)
    expect(worker.postMessage).toHaveBeenCalledExactlyOnceWith({ type: 'failed' })
  })

  it.each(['throw', 'type', 'length'] as const)('reports %s inference failure to the bridge and main thread', reason => {
    send(init)
    const active = port()
    send({ type: 'connect', port: active })
    if (reason === 'throw') glue.df_process_frame.mockImplementationOnce(() => { throw new Error('inference failed') })
    if (reason === 'type') glue.df_process_frame.mockReturnValueOnce([])
    if (reason === 'length') glue.df_process_frame.mockReturnValueOnce(new Float32Array(128))
    const frame: DfnFrame = { type: 'frame', id: 7, samples: new Float32Array(480) }
    active.onmessage({ data: frame })
    expect(active.postMessage).toHaveBeenCalledExactlyOnceWith({ type: 'failed' })
    expect(worker.postMessage).toHaveBeenLastCalledWith({ type: 'failed' })
  })
})
