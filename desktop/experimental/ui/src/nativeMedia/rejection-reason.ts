/** Match fixed upstream diagnostics locally; never expose their raw text. */
export function rejectionReason(error: unknown): string {
  if (error instanceof WebAssembly.RuntimeError) return 'wasm_runtime_trap'
  if (!(error instanceof Error)) return 'unknown_crypto_error'
  const message = error.message
  if (message === 'Failed to Decrypt') return 'authentication_failed'
  if (message === 'Failed to Encrypt') return 'encryption_failed'
  if (message === 'unauthorized SFrame key id') return 'unknown_key_id'
  if (message === 'empty Opus frame' || message === 'empty frame') return 'empty_codec_frame'
  if (message === 'truncated VP8 header') return 'truncated_codec_prefix'
  if (message === 'truncated SFrame') return 'truncated_sframe'
  if (/^buffer with size \d+ is too small$/.test(message)) return 'truncated_sframe_header'
  if (/^Frame \d+ of key id \d+ was rejected, as it was duplicated$/.test(message)) return 'counter_replay'
  if (/^Frame \d+ of key id \d+ was rejected, as its counter is too old$/.test(message)) return 'counter_too_old'
  if (message === 'unsupported codec') return 'unsupported_codec'
  if (message === 'protected_context_missing') return 'context_missing'
  return 'unknown_crypto_error'
}
