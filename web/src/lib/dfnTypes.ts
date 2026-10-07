/** Local protocol between the owned DFN Worker, render bridge and node. */
export type NoiseModel = 'dfn3' | 'gtcrn'
export type DeepFilterAssets = readonly [WebAssembly.Module, ArrayBuffer]
export interface FilterOptions { signal?: AbortSignal; onFailure?: () => void }
export interface FilterStats { processedFrames: number; droppedFrames: number; bridgeBudgetMs: number }
export interface DfnFrame { type: 'frame'; id: number; samples: Float32Array<ArrayBuffer> }
export interface DfnFailed { type: 'failed' }
export interface DfnConnect { type: 'connect'; port: MessagePort }
export interface DfnReady { type: 'ready'; frameLength: number }
export interface DfnInit { type: 'init'; wasmModule: WebAssembly.Module; modelBytes: ArrayBuffer; suppressionLevel: number }
export type DfnWorkerCommand = DfnInit | DfnConnect
export type DfnWorkerReply = DfnReady | DfnFailed
export type DfnBridgeCommand = DfnConnect | DfnFailed
export type DfnFrameReply = DfnFrame | DfnFailed
export type DfnBridgeReport = DfnFailed | ({ type: 'stats' } & FilterStats)

function tagged(value: unknown, type: string): value is { type: string } {
  return typeof value === 'object' && value !== null && 'type' in value && value.type === type
}
export function isDfnConnect(value: unknown): value is DfnConnect {
  return tagged(value, 'connect') && 'port' in value && typeof value.port === 'object' && value.port !== null
    && 'postMessage' in value.port && typeof value.port.postMessage === 'function'
    && 'close' in value.port && typeof value.port.close === 'function'
}
export function isDfnInit(value: unknown): value is DfnInit {
  return tagged(value, 'init') && 'wasmModule' in value && value.wasmModule instanceof WebAssembly.Module
    && 'modelBytes' in value && value.modelBytes instanceof ArrayBuffer
    && 'suppressionLevel' in value && typeof value.suppressionLevel === 'number'
}
export function isDfnFailed(value: unknown): value is DfnFailed { return tagged(value, 'failed') }
export function isDfnReady(value: unknown): value is DfnReady {
  return tagged(value, 'ready') && 'frameLength' in value && typeof value.frameLength === 'number'
}
export function isDfnStats(value: unknown): value is { type: 'stats' } & FilterStats {
  return tagged(value, 'stats') && 'processedFrames' in value && Number.isSafeInteger(value.processedFrames)
    && 'droppedFrames' in value && Number.isSafeInteger(value.droppedFrames)
    && 'bridgeBudgetMs' in value && typeof value.bridgeBudgetMs === 'number'
}
/** Validate the frame header before the existing sample size/type rejection. */
export function isDfnFrameHeader(value: unknown): value is { type: 'frame'; id: number; samples: unknown } {
  return tagged(value, 'frame') && 'id' in value && typeof value.id === 'number' && Number.isSafeInteger(value.id) && 'samples' in value
}
