# DeepFilterNet3 (vendored)

Full-band AI noise suppression for the microphone, used by
`web/src/lib/noiseSuppressor.js` (noise mode "AI filter"). Runs entirely in
the browser in a dedicated Worker with an AudioWorklet bridge; the files
below are served by our own binary.

| File | Origin | SHA-256 |
|---|---|---|
| `df_bg.wasm` | libDF compiled to WebAssembly by mezon-noise-suppression (`v3/pkg/df_bg.wasm` of its asset CDN) | `440b5d12b6ea7d95008736f844221d7874ee15de5cb10d3015002470fdba0432` |
| `DeepFilterNet3_onnx.tgz` | DeepFilterNet3 ONNX export (`v3/models/DeepFilterNet3_onnx.tar.gz`, renamed so dev servers and proxies don't serve it with `Content-Encoding: gzip`; libDF needs the compressed bytes) | `c94d91f70911001c946e0fabb4aa9adc37045f45a03b56008cb0c8244cb63616` |
| `worklet.js` | The `workletCode` string of `deepfilternet3-noise-filter@1.3.0` (`dist/index.esm.js`), written to a file so the CSP doesn't need `blob:` scripts; only a license header was added | – |
| `worker-glue.js` | Mnema adaptation of the same licensed glue: removes the AudioWorklet processor and exports its existing WASM functions for a dedicated Worker; model/algorithms unchanged | – |

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

Replace the upstream assets and update the checksums above. Regenerate the
adapted `worker-glue.js` from the matching licensed WASM glue, retaining its
license header and exports consumed by `web/src/lib/dfnWorker.js`. Verify
the WASM API, 480-sample frame length, model compatibility and actual Worker
output in browser tests. Keep the `deepfilter-worker-bridge` processor name
consistent between `web/src/lib/dfnBridgeWorklet.js` and `dfnNode.js`.

The active DFN3 pipeline uses `worker-glue.js` in a dedicated Worker for model
initialization and inference. A small `web/src/lib/dfnBridgeWorklet.js` only
copies mono PCM; it does not decompress weights or run the neural network on
the audio rendering thread. At 48 kHz it sends 480-sample frames, limits
in-flight work and queued output to three frames, and discards output beyond
the 30 ms bridge budget. This budget excludes the model's intrinsic delay,
capture, encoding and the network. Temporary scheduling gaps bypass filtering
without extending that buffer; a sustained 250 ms outage is treated as failure.
Persistent failure produces a visible
warning and native browser suppression, while retaining the user's AI choice.
The original `worklet.js` remains as provenance and a diagnostic comparison;
it is no longer imported by the production pipeline.
