import { isDfnConnect, isDfnInit, isDfnFrameHeader } from './dfnTypes'
// Model setup and inference run away from the browser's audio rendering thread.
import { initSync, df_create, df_get_frame_length, df_process_frame } from '../third_party/deepfilternet3/worker-glue.js'

let handle: number | null = null
let frameLength = 0
let port: MessagePort | null = null

self.onmessage = ({ data }: MessageEvent<unknown>) => {
  try {
    if (isDfnInit(data) && handle === null) {
      initSync({ module: data.wasmModule })
      handle = df_create(new Uint8Array(data.modelBytes), data.suppressionLevel)
      frameLength = df_get_frame_length(handle)
      if (frameLength !== 480) throw new Error('Unexpected DeepFilterNet frame length')
      self.postMessage({ type: 'ready', frameLength })
    } else if (isDfnConnect(data) && handle !== null && !port) {
      const framePort = data.port
      const activeHandle = handle
      port = framePort
      framePort.onmessage = ({ data: frame }: MessageEvent<unknown>) => {
        if (!isDfnFrameHeader(frame) || !(frame.samples instanceof Float32Array) || frame.samples.length !== frameLength) return
        try {
          const samples = df_process_frame(activeHandle, frame.samples)
          if (!(samples instanceof Float32Array) || samples.length !== frameLength) throw new Error('Invalid filter output')
          framePort.postMessage({ type: 'frame', id: frame.id, samples }, [samples.buffer])
        } catch {
          framePort.postMessage({ type: 'failed' })
          self.postMessage({ type: 'failed' })
        }
      }
    }
  } catch {
    self.postMessage({ type: 'failed' })
  }
}
