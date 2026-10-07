// Model setup and inference run away from the browser's audio rendering thread.
import { initSync, df_create, df_get_frame_length, df_process_frame } from '../third_party/deepfilternet3/worker-glue.js'

let handle = null
let frameLength = 0
let port = null

self.onmessage = ({ data }) => {
  try {
    if (data.type === 'init' && handle === null) {
      initSync({ module: data.wasmModule })
      handle = df_create(new Uint8Array(data.modelBytes), data.suppressionLevel)
      frameLength = df_get_frame_length(handle)
      if (frameLength !== 480) throw new Error('Unexpected DeepFilterNet frame length')
      self.postMessage({ type: 'ready', frameLength })
    } else if (data.type === 'connect' && handle !== null && !port) {
      port = data.port
      port.onmessage = ({ data: frame }) => {
        if (frame.type !== 'frame' || !(frame.samples instanceof Float32Array) || frame.samples.length !== frameLength) return
        try {
          const samples = df_process_frame(handle, frame.samples)
          if (!(samples instanceof Float32Array) || samples.length !== frameLength) throw new Error('Invalid filter output')
          port.postMessage({ type: 'frame', id: frame.id, samples }, [samples.buffer])
        } catch {
          port.postMessage({ type: 'failed' })
          self.postMessage({ type: 'failed' })
        }
      }
    }
  } catch {
    self.postMessage({ type: 'failed' })
  }
}
