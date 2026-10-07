// Reviewed declarations for matching licensed worker-glue.js exports.
// Generated WASM glue stays JavaScript; it is not reimplemented here.
export function initSync(input: { module: WebAssembly.Module | BufferSource }): WebAssembly.Exports
export function df_create(modelBytes: Uint8Array, attenuationLimitDb: number): number
export function df_get_frame_length(state: number): number
export function df_process_frame(state: number, input: Float32Array): Float32Array<ArrayBuffer>
