import type { DeepFilterAssets, FilterOptions, FilterStats } from './dfnTypes'
import { isDfnReady, isDfnFailed, isDfnStats } from './dfnTypes'

export interface FilterNode extends AudioWorkletNode { destroy?: () => void; filterStats?: FilterStats }

// No model setup or inference takes place in the audio rendering thread.
export async function createDeepFilterNode(ctx: AudioContext, assets: DeepFilterAssets, { signal, onFailure }: FilterOptions = {}): Promise<FilterNode | null> {
  const canceled = () => signal?.aborted === true || ctx.state === 'closed'
  if (canceled()) return null
  const worker = new Worker(new URL('./dfnWorker.ts', import.meta.url), { type: 'module' })
  let node: FilterNode | null = null
  let channel: MessageChannel | null = null
  let destroyed = false
  let failed = false
  let rejectReady: ((reason: Error) => void) | null = null
  let readyTimeout: ReturnType<typeof setTimeout> | undefined
  const abort = () => {
    rejectReady?.(new Error('Filter initialization canceled'))
    destroy()
  }
  function destroy() {
    if (destroyed) return
    destroyed = true
    clearTimeout(readyTimeout)
    worker.terminate()
    signal?.removeEventListener('abort', abort)
    channel?.port1.close()
    channel?.port2.close()
    try { node?.disconnect() } catch { /* already disconnected */ }
  }
  const fail = () => {
    if (destroyed || failed) return
    failed = true
    rejectReady?.(new Error('Filter worker unavailable'))
    if (node) {
      node.port.postMessage({ type: 'failed' })
      worker.terminate()
      onFailure?.()
    }
  }
  signal?.addEventListener('abort', abort, { once: true })
  try {
    await new Promise<void>((resolve, reject) => {
      rejectReady = reject
      readyTimeout = setTimeout(() => reject(new Error('Filter initialization timed out')), 15000)
      worker.onmessage = ({ data }: MessageEvent<unknown>) => {
        if (isDfnReady(data)) {
          clearTimeout(readyTimeout)
          if (data.frameLength !== 480) { reject(new Error('Unexpected filter frame size')); return }
          resolve()
        } else if (isDfnFailed(data)) {
          clearTimeout(readyTimeout)
          fail()
        }
      }
      worker.onerror = () => { clearTimeout(readyTimeout); fail() }
      const [wasmModule, modelBytes] = assets
      worker.postMessage({ type: 'init', wasmModule, modelBytes, suppressionLevel: 100 })
    })
    rejectReady = null
    if (destroyed || canceled()) { destroy(); return null }
    const activeNode: FilterNode = new AudioWorkletNode(ctx, 'deepfilter-worker-bridge', { numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1] })
    node = activeNode
    activeNode.filterStats = { processedFrames: 0, droppedFrames: 0, bridgeBudgetMs: 30 }
    channel = new MessageChannel()
    activeNode.port.onmessage = ({ data }: MessageEvent<unknown>) => {
      if (isDfnFailed(data)) fail()
      else if (isDfnStats(data)) activeNode.filterStats = {
        processedFrames: data.processedFrames, droppedFrames: data.droppedFrames, bridgeBudgetMs: data.bridgeBudgetMs
      }
    }
    activeNode.onprocessorerror = fail
    activeNode.destroy = destroy
    worker.postMessage({ type: 'connect', port: channel.port2 }, [channel.port2])
    activeNode.port.postMessage({ type: 'connect', port: channel.port1 }, [channel.port1])
    return activeNode
  } catch (err) {
    destroy()
    if (canceled()) return null
    throw err
  }
}
