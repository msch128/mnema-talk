import { initSync, MediaCryptor } from './crypto/mnema_media_spike.js'
import wasmBase64 from './crypto/wasm-bytes'
import { checkSframe } from './sframe-checks'
import { createProtectedContext } from './protected-context'
import { rejectionReason } from './rejection-reason'
import { processProtectedFrame } from './protected-frame'

const scope = self as DedicatedWorkerGlobalScope
const moduleBytes = Uint8Array.from(atob(wasmBase64), character => character.charCodeAt(0))
initSync({ module: moduleBytes })
const selftests = checkSframe()
const consumedContexts = new Set<number>()
const tamperNext = new Set<string>()
scope.addEventListener('message', event => {
  const data = event.data as { type?: string; id?: string }
  if (data.type === 'tamper-next' && data.id === 'send-audio') tamperNext.add(data.id)
})
scope.addEventListener('rtctransform', event => {
  const transformer = event.transformer
  const { id, sframe, key, context } = transformer.options as { id: string; sframe: boolean; key?: Uint8Array; context?: number }
  const sending = id.startsWith('send-')
  const codec = id.endsWith('video') ? 'vp8' : 'opus'
  let cryptor: MediaCryptor | undefined
  if (sframe) {
    if (!key || key.length !== 32 || (context !== 1 && context !== 2)) {
      scope.postMessage({ id, status: 'protected_setup_rejected', frames: 0, bytes: 0, drops: 1 })
      return
    }
    try { cryptor = createProtectedContext(key, context, consumedContexts) }
    catch {
      key.fill(0)
      scope.postMessage({ id, status: 'protected_setup_rejected', frames: 0, bytes: 0, drops: 1 })
      return
    }
    key.fill(0)
  }
  let frames = 0
  let bytes = 0
  let drops = 0
  let nativeEmptyFrames = 0
  let tamperInjected = 0
  const rejectionCounts: Record<string, number> = {}
  const rejectionInputLengthClasses = { zero: 0, nonzero: 0 }
  scope.postMessage({ id, status: 'attached', frames, bytes, drops })
  void transformer.readable.pipeThrough(new TransformStream<RTCEncodedVideoFrame | RTCEncodedAudioFrame, RTCEncodedVideoFrame | RTCEncodedAudioFrame>({
    transform(frame, controller) {
      if (sframe) {
        try {
          const input = new Uint8Array(frame.data)
          const result = processProtectedFrame(cryptor, input, codec, sending)
          if (result.kind === 'native-empty-audio-discard') {
            nativeEmptyFrames++
            // No crypto call, counter transition, authenticated-frame count or enqueue.
            scope.postMessage({ id, status: 'native_empty_audio_discarded', frames, bytes, drops, rejectionCounts, rejectionInputLengthClasses, nativeEmptyFrames, tamperInjected })
            return
          }
          const output = result.output.slice()
          if (sending && tamperNext.delete(id)) {
            output[output.length - 1]! ^= 1
            tamperInjected++
            scope.postMessage({ id, status: 'tamper_injected_after_encryption', frames, bytes, drops, rejectionCounts, rejectionInputLengthClasses, nativeEmptyFrames, tamperInjected })
          }
          frame.data = output.buffer
        } catch (error) {
          drops++
          rejectionInputLengthClasses[frame.data.byteLength === 0 ? 'zero' : 'nonzero']++
          const reason = rejectionReason(error)
          rejectionCounts[reason] = (rejectionCounts[reason] ?? 0) + 1
          // Never enqueue the original frame on any protected-path failure.
          scope.postMessage({ id, status: 'protected_frame_rejected', frames, bytes, drops, rejectionCounts, rejectionInputLengthClasses, nativeEmptyFrames, tamperInjected })
          return
        }
      }
      frames++
      bytes += frame.data.byteLength
      controller.enqueue(frame)
      if (frames === 1 || frames % 20 === 0) scope.postMessage({ id, status: sframe ? (sending ? 'encrypted' : 'authenticated') : 'flowing', frames, bytes, drops, rejectionCounts, rejectionInputLengthClasses, nativeEmptyFrames, tamperInjected })
    }
  })).pipeTo(transformer.writable).then(
    () => scope.postMessage({ id, status: 'ended', frames, bytes, drops, rejectionCounts, rejectionInputLengthClasses, nativeEmptyFrames, tamperInjected }),
    () => scope.postMessage({ id, status: 'pipeline_error', frames, bytes, drops, rejectionCounts, rejectionInputLengthClasses, nativeEmptyFrames, tamperInjected })
  ).finally(() => { cryptor?.free() })
})
scope.postMessage({ id: scope.name || 'worker', status: 'ready', frames: 0, bytes: 0, drops: 0, selftests })
