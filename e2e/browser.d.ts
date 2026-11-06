export {}

declare global {
  interface Window {
    __pcs: RTCPeerConnection[]
    __pcTimings: Record<string, number>[]
    __dfnWorkers: Array<{ ready: boolean; failed: boolean; why: string }>
    __dfnNodes: Array<{ node: AudioWorkletNode & { filterStats?: { processedFrames: number } }; failed: boolean; why: string }>
    __micRequests: number
    __pip: { requests: number; exits: number }
  }
}
