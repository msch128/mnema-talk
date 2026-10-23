import type { CryptoStateStore } from '../web/src/lib/crypto/stateStore'

declare global {
  interface Window {
    __pcs: RTCPeerConnection[]
    __dfnWorkers: Array<{ ready: boolean; failed: boolean; why: string }>
    __dfnNodes: Array<{ node: AudioWorkletNode & { filterStats?: { processedFrames: number } }; failed: boolean; why: string }>
    __micRequests: number
    __pip: { requests: number; exits: number }
    openStore: (fresh: string) => Promise<CryptoStateStore>
    store: CryptoStateStore
  }
}
