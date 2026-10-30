// Counters for the idle-cost spec (perf-idle.spec.ts), injected into every page
// before the app loads. It counts what wakes the page up (timers, animation
// frames, socket frames), which audio and media objects stay alive, and lets
// the spec pretend that the tab is hidden: headless Chromium has no real
// hidden state, so document.hidden / visibilityState are overridden and a
// visibilitychange event is sent, exactly what the app reacts to.

export interface TimerSite { site: string; delay: number; fires: number; live: boolean }
export interface ProbeSnapshot {
  intervalFires: number
  timeoutFires: number
  rafFires: number
  wsSent: number
  wsReceived: number
  sites: TimerSite[]
  liveIntervals: number
  audioContexts: string[]
  liveTracks: number
  liveWorkers: number
  hidden: boolean
  wsTypes: Record<string, number>
  mutations: Record<string, number>
}

export function installPerfProbe(): void {
  const w = window as unknown as Record<string, unknown>
  const sites = new Map<string, TimerSite>()
  const live = new Map<number, string>()
  const state = { intervalFires: 0, timeoutFires: 0, rafFires: 0, wsSent: 0, wsReceived: 0, hidden: false }
  const tracks: MediaStreamTrack[] = []
  const contexts: AudioContext[] = []
  const workers = new Set<Worker>()
  const wsTypes: Record<string, number> = {}
  const mutations: Record<string, number> = {}
  const countType = (data: unknown, prefix: string) => {
    if (typeof data !== 'string') return
    const type = /"type":"([a-z_]+)"/.exec(data)?.[1] ?? '?'
    wsTypes[prefix + type] = (wsTypes[prefix + type] ?? 0) + 1
  }
  // Which parts of the page change on their own: the style recalculations and layouts come from these.
  new MutationObserver((records) => {
    for (const r of records) {
      const el = r.target instanceof Element ? r.target : r.target.parentElement
      const key = `${r.type}:${r.attributeName ?? ''} ${el?.tagName.toLowerCase() ?? '?'}.${String(el?.getAttribute('class') ?? '').slice(0, 50)}`
      mutations[key] = (mutations[key] ?? 0) + 1
    }
  }).observe(document, { subtree: true, childList: true, attributes: true, characterData: true })

  const siteOf = (delay: number): TimerSite => {
    const frames = (new Error().stack ?? '').split('\n').slice(3, 6).map((l) => l.trim().replace(/^at /, '').replace(/https?:\/\/[^/]+/, ''))
    const key = `${delay}|${frames.join(' < ')}`
    let site = sites.get(key)
    if (!site) { site = { site: frames.join(' < '), delay, fires: 0, live: true }; sites.set(key, site) }
    return site
  }

  const nativeSetInterval = window.setInterval.bind(window)
  const nativeClearInterval = window.clearInterval.bind(window)
  const nativeSetTimeout = window.setTimeout.bind(window)
  window.setInterval = ((handler: TimerHandler, delay?: number, ...args: unknown[]) => {
    const site = siteOf(delay ?? 0)
    const id = nativeSetInterval(() => { state.intervalFires++; site.fires++; if (typeof handler === 'function') handler(...args) }, delay)
    live.set(id, site.site + '|' + (delay ?? 0))
    return id
  }) as typeof window.setInterval
  window.clearInterval = ((id?: number) => {
    if (id !== undefined) live.delete(id)
    nativeClearInterval(id)
  }) as typeof window.clearInterval
  window.setTimeout = ((handler: TimerHandler, delay?: number, ...args: unknown[]) => {
    const site = siteOf(delay ?? 0)
    return nativeSetTimeout(() => { state.timeoutFires++; site.fires++; if (typeof handler === 'function') handler(...args) }, delay)
  }) as typeof window.setTimeout
  const nativeRaf = window.requestAnimationFrame.bind(window)
  window.requestAnimationFrame = (cb: FrameRequestCallback) => nativeRaf((t) => { state.rafFires++; cb(t) })

  const NativeWebSocket = window.WebSocket
  window.WebSocket = class extends NativeWebSocket {
    constructor(...args: ConstructorParameters<typeof WebSocket>) {
      super(...args)
      this.addEventListener('message', (e) => { state.wsReceived++; countType(e.data, 'in:') })
    }
    override send(data: string | ArrayBuffer | Blob | ArrayBufferView<ArrayBuffer>): void { state.wsSent++; countType(data, 'out:'); super.send(data) }
  }

  const NativeAudioContext = window.AudioContext
  window.AudioContext = class extends NativeAudioContext {
    constructor(...args: ConstructorParameters<typeof AudioContext>) { super(...args); contexts.push(this) }
  }
  const NativeWorker = window.Worker
  window.Worker = class extends NativeWorker {
    constructor(...args: ConstructorParameters<typeof Worker>) { super(...args); workers.add(this) }
    override terminate(): void { workers.delete(this); super.terminate() }
  }
  const devices = navigator.mediaDevices
  for (const name of ['getUserMedia', 'getDisplayMedia'] as const) {
    const native = devices[name].bind(devices)
    devices[name] = async (constraints?: MediaStreamConstraints | DisplayMediaStreamOptions) => {
      const stream = await native(constraints)
      tracks.push(...stream.getTracks())
      return stream
    }
  }

  Object.defineProperty(document, 'hidden', { configurable: true, get: () => state.hidden })
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (state.hidden ? 'hidden' : 'visible') })
  w.__perfSetHidden = (hidden: boolean) => {
    state.hidden = hidden
    document.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event(hidden ? 'blur' : 'focus'))
  }
  w.__perfSnapshot = (): ProbeSnapshot => ({
    ...state,
    wsTypes: { ...wsTypes },
    mutations: { ...mutations },
    sites: [...sites.values()].map((s) => ({ ...s })),
    liveIntervals: live.size,
    audioContexts: contexts.map((c) => c.state),
    liveTracks: tracks.filter((t) => t.readyState === 'live').length,
    liveWorkers: workers.size,
  })
}
