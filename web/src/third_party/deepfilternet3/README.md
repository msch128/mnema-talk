# DeepFilterNet3 (vendored)

Full-band AI noise suppression for the microphone, used by
`web/src/lib/noiseSuppressor.js` (noise mode "AI filter"). Runs entirely in
the browser as an AudioWorklet; the files below are served by our own binary.

| File | Origin | SHA-256 |
|---|---|---|
| `df_bg.wasm` | libDF compiled to WebAssembly by mezon-noise-suppression (`v3/pkg/df_bg.wasm` of its asset CDN) | `440b5d12b6ea7d95008736f844221d7874ee15de5cb10d3015002470fdba0432` |
| `DeepFilterNet3_onnx.tgz` | DeepFilterNet3 ONNX export (`v3/models/DeepFilterNet3_onnx.tar.gz`, renamed so dev servers and proxies don't serve it with `Content-Encoding: gzip`; libDF needs the compressed bytes) | `c94d91f70911001c946e0fabb4aa9adc37045f45a03b56008cb0c8244cb63616` |
| `worklet.js` | The `workletCode` string of `deepfilternet3-noise-filter@1.3.0` (`dist/index.esm.js`), written to a file so the CSP doesn't need `blob:` scripts; only a license header was added | – |

Downloaded 2026-10-05 from the asset location hard-coded in
`deepfilternet3-noise-filter@1.3.0`.

## Licenses

- DeepFilterNet / libDF and the model weights, © Hendrik Schröter:
  MIT OR Apache-2.0 (https://github.com/Rikorose/DeepFilterNet)
- deepfilternet3-noise-filter / mezon-noise-suppression:
  MIT OR Apache-2.0 (https://github.com/mezonai/mezon-noise-suppression)
- Rust crates compiled into `df_bg.wasm` (tract, ndarray, rustfft, realfft,
  flate2, tar, serde and others): MIT and/or Apache-2.0, some also Unlicense
  or Zlib; see THIRD_PARTY_NOTICES.md in the repository root.

`LICENSE-MIT` and `LICENSE-APACHE` are the license texts shipped with the
npm package.

## Updating

Replace the files, update the checksums above, and keep the worklet's
processor name (`deepfilter-audio-processor`) and `processorOptions`
(`wasmModule`, `modelBytes`, `suppressionLevel`) in sync with
`noiseSuppressor.js`.
