// No model setup or inference takes place in the audio rendering thread.
export async function createDeepFilterNode(ctx, assets, { signal, onFailure } = {}) {
  if (signal?.aborted || ctx.state === 'closed') return null
  const worker = new Worker(new URL('./dfnWorker.js', import.meta.url), { type: 'module' })
  let node = null
  let channel = null
  let destroyed = false
  let failed = false
  let rejectReady
  let readyTimeout
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
    await new Promise((resolve, reject) => {
      rejectReady = reject
      readyTimeout = setTimeout(() => reject(new Error('Filter initialization timed out')), 15000)
      worker.onmessage = ({ data }) => {
        if (data.type === 'ready') {
          clearTimeout(readyTimeout)
          if (data.frameLength !== 480) { reject(new Error('Unexpected filter frame size')); return }
          resolve()
        } else if (data.type === 'failed') {
          clearTimeout(readyTimeout)
          fail()
        }
      }
      worker.onerror = () => { clearTimeout(readyTimeout); fail() }
      const [wasmModule, modelBytes] = assets
      worker.postMessage({ type: 'init', wasmModule, modelBytes, suppressionLevel: 100 })
    })
    rejectReady = null
    if (destroyed || signal?.aborted || ctx.state === 'closed') { destroy(); return null }
    node = new AudioWorkletNode(ctx, 'deepfilter-worker-bridge', { numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1] })
    node.filterStats = { processedFrames: 0, droppedFrames: 0, bridgeBudgetMs: 30 }
    channel = new MessageChannel()
    node.port.onmessage = ({ data }) => {
      if (data.type === 'failed') fail()
      else if (data.type === 'stats') node.filterStats = {
        processedFrames: data.processedFrames, droppedFrames: data.droppedFrames, bridgeBudgetMs: data.bridgeBudgetMs
      }
    }
    node.onprocessorerror = fail
    node.destroy = destroy
    worker.postMessage({ type: 'connect', port: channel.port2 }, [channel.port2])
    node.port.postMessage({ type: 'connect', port: channel.port1 }, [channel.port1])
    return node
  } catch (err) {
    destroy()
    if (signal?.aborted || ctx.state === 'closed') return null
    throw err
  }
}
