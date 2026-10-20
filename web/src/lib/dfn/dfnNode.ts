import type { DeepFilterAssets, FilterOptions, FilterStats } from './dfnTypes'
import { isDfnReady, isDfnFailed, isDfnStats } from './dfnTypes'

export interface FilterNode extends AudioWorkletNode { destroy?: () => void; filterStats?: FilterStats }

function isFilterCanceled(signal?: AbortSignal, ctx?: AudioContext): boolean {
  if (signal?.aborted === true) return true
  return ctx?.state === 'closed'
}

class FilterLifecycle {
  node: FilterNode | null = null
  channel: MessageChannel | null = null
  destroyed = false
  failed = false
  rejectReady: ((reason: Error) => void) | null = null

  constructor(
    private worker: Worker,
    private signal?: AbortSignal,
    private onFailure?: () => void
  ) {
    this.signal?.addEventListener('abort', this.abort, { once: true })
  }

  abort = () => {
    this.rejectReady?.(new Error('Filter initialization canceled'))
    this.destroy()
  }

  destroy = () => {
    if (this.destroyed) return
    this.destroyed = true
    this.worker.terminate()
    this.signal?.removeEventListener('abort', this.abort)
    this.channel?.port1.close()
    this.channel?.port2.close()
    try { this.node?.disconnect() } catch (err) { void err }
  }

  fail = () => {
    if (this.destroyed || this.failed) return
    this.failed = true
    this.rejectReady?.(new Error('Filter worker unavailable'))
    if (this.node) {
      this.node.port.postMessage({ type: 'failed' })
      this.worker.terminate()
      this.onFailure?.()
    }
  }

  setRejectReady(rj: ((reason: Error) => void) | null) {
    this.rejectReady = rj
  }
}

function initFilterWorker(
  worker: Worker,
  assets: DeepFilterAssets,
  onFail: (err: Error) => void,
  registerReject: (rj: (reason: Error) => void) => void
): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    registerReject(reject)
    const readyTimeout = setTimeout(() => reject(new Error('Filter initialization timed out')), 15000)
    worker.onmessage = ({ data }: MessageEvent<unknown>) => {
      if (isDfnReady(data)) {
        clearTimeout(readyTimeout)
        if (data.frameLength !== 480) {
          reject(new Error('Unexpected filter frame size'))
          return
        }
        resolve()
      } else if (isDfnFailed(data)) {
        clearTimeout(readyTimeout)
        onFail(new Error('Filter worker unavailable'))
      }
    }
    worker.onerror = () => {
      clearTimeout(readyTimeout)
      onFail(new Error('Filter worker unavailable'))
    }
    const [wasmModule, modelBytes] = assets
    worker.postMessage({ type: 'init', wasmModule, modelBytes, suppressionLevel: 100 })
  })
}

function wireWorkerRuntime(worker: Worker, fail: () => void) {
  worker.onmessage = ({ data }: MessageEvent<unknown>) => {
    if (isDfnFailed(data)) fail()
  }
  worker.onerror = () => fail()
}

function setupBridgeNode(
  ctx: AudioContext,
  channel: MessageChannel,
  destroy: () => void,
  fail: () => void
): FilterNode {
  const activeNode: FilterNode = new AudioWorkletNode(ctx, 'deepfilter-worker-bridge', {
    numberOfInputs: 1,
    numberOfOutputs: 1,
    outputChannelCount: [1]
  })
  activeNode.filterStats = { processedFrames: 0, droppedFrames: 0, bridgeBudgetMs: 30 }
  activeNode.port.onmessage = ({ data }: MessageEvent<unknown>) => {
    if (isDfnFailed(data)) fail()
    else if (isDfnStats(data)) activeNode.filterStats = {
      processedFrames: data.processedFrames, droppedFrames: data.droppedFrames, bridgeBudgetMs: data.bridgeBudgetMs
    }
  }
  activeNode.onprocessorerror = fail
  activeNode.destroy = destroy
  return activeNode
}

// No model setup or inference takes place in the audio rendering thread.
export async function createDeepFilterNode(
  ctx: AudioContext,
  assets: DeepFilterAssets,
  { signal, onFailure }: FilterOptions = {}
): Promise<FilterNode | null> {
  if (isFilterCanceled(signal, ctx)) return null

  const worker = new Worker(new URL('./dfnWorker.ts', import.meta.url), { type: 'module' })
  const lifecycle = new FilterLifecycle(worker, signal, onFailure)

  try {
    await initFilterWorker(worker, assets, err => {
      lifecycle.rejectReady?.(err)
      lifecycle.fail()
    }, rj => lifecycle.setRejectReady(rj))
    lifecycle.setRejectReady(null)

    if (lifecycle.destroyed || isFilterCanceled(signal, ctx)) {
      lifecycle.destroy()
      return null
    }

    const channel = new MessageChannel()
    lifecycle.channel = channel
    const node = setupBridgeNode(ctx, channel, lifecycle.destroy, lifecycle.fail)
    lifecycle.node = node
    wireWorkerRuntime(worker, lifecycle.fail)

    worker.postMessage({ type: 'connect', port: channel.port2 }, [channel.port2])
    node.port.postMessage({ type: 'connect', port: channel.port1 }, [channel.port1])
    return node
  } catch (err) {
    lifecycle.destroy()
    if (isFilterCanceled(signal, ctx)) return null
    throw err
  }
}
