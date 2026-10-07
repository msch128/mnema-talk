import type { CryptoStateStore } from '../web/src/lib/crypto/stateStore'

declare global {
  interface Window {
    __pcs: RTCPeerConnection[]
    __dfnWorkers: Array<{ ready: boolean; failed: boolean }>
    __dfnNodes: Array<{ node: AudioWorkletNode & { filterStats?: { processedFrames: number } }; failed: boolean }>
    __pip: { requests: number; exits: number }
    openStore: (fresh: string) => Promise<CryptoStateStore>
    store: CryptoStateStore
  }
}
