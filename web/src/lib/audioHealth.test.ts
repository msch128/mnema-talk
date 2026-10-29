import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CONTEXT_START_MS, FILTER_FRAMES_MS, isPageUnloading, onPageLeave, watchContextStart, watchFilterFrames } from './audioHealth'

class FakeContext extends EventTarget {
  state: AudioContextState = 'suspended'
  run() { this.state = 'running'; this.dispatchEvent(new Event('statechange')) }
}
const context = () => new FakeContext()
const asContext = (ctx: FakeContext) => ctx as unknown as AudioContext
const pageEvent = (type: string, persisted: boolean) => window.dispatchEvent(Object.assign(new Event(type), { persisted }))

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers(); pageEvent('pageshow', false) })

describe('watching an audio context start', () => {
  it('reports a context that is still not rendering after the timeout', () => {
    const onStall = vi.fn()
    watchContextStart(asContext(context()), undefined, onStall)
    vi.advanceTimersByTime(CONTEXT_START_MS - 1)
    expect(onStall).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(onStall).toHaveBeenCalledExactlyOnceWith('context-not-running')
  })

  it('stays quiet for a context that starts in time or already runs', () => {
    const onStall = vi.fn()
    const late = context()
    watchContextStart(asContext(late), undefined, onStall)
    vi.advanceTimersByTime(CONTEXT_START_MS - 100)
    late.run()
    const running = context()
    running.run()
    watchContextStart(asContext(running), undefined, onStall)
    vi.advanceTimersByTime(CONTEXT_START_MS * 2)
    expect(onStall).not.toHaveBeenCalled()
  })

  it('stays quiet once the pipeline was torn down or the page is being left', () => {
    const onStall = vi.fn()
    const controller = new AbortController()
    watchContextStart(asContext(context()), controller.signal, onStall)
    controller.abort()
    const leaving = context()
    watchContextStart(asContext(leaving), undefined, onStall)
    pageEvent('pagehide', false)
    expect(isPageUnloading()).toBe(true)
    vi.advanceTimersByTime(CONTEXT_START_MS * 2)
    expect(onStall).not.toHaveBeenCalled()
  })

  it('stays quiet for a context that was closed meanwhile', () => {
    const onStall = vi.fn()
    const ctx = context()
    watchContextStart(asContext(ctx), undefined, onStall)
    ctx.state = 'closed'
    vi.advanceTimersByTime(CONTEXT_START_MS * 2)
    expect(onStall).not.toHaveBeenCalled()
  })
})

describe('watching a filter deliver frames', () => {
  it('reports a running filter that processed nothing', () => {
    const onStall = vi.fn()
    const ctx = context()
    ctx.run()
    watchFilterFrames(asContext(ctx), { filterStats: { processedFrames: 0 } }, undefined, onStall)
    vi.advanceTimersByTime(FILTER_FRAMES_MS - 200)
    expect(onStall).not.toHaveBeenCalled()
    vi.advanceTimersByTime(300)
    expect(onStall).toHaveBeenCalledExactlyOnceWith('no-frames')
    vi.advanceTimersByTime(FILTER_FRAMES_MS)
    expect(onStall).toHaveBeenCalledOnce()
  })

  it('counts from the moment the context runs, not from the node', () => {
    const onStall = vi.fn()
    const ctx = context()
    watchFilterFrames(asContext(ctx), {}, undefined, onStall)
    vi.advanceTimersByTime(FILTER_FRAMES_MS * 3)
    expect(onStall).not.toHaveBeenCalled()
    ctx.run()
    vi.advanceTimersByTime(FILTER_FRAMES_MS + 200)
    expect(onStall).toHaveBeenCalledOnce()
  })

  it('is satisfied by the first processed frame', () => {
    const onStall = vi.fn()
    const ctx = context()
    ctx.run()
    const node = { filterStats: { processedFrames: 0 } }
    watchFilterFrames(asContext(ctx), node, undefined, onStall)
    vi.advanceTimersByTime(1000)
    node.filterStats.processedFrames = 1
    vi.advanceTimersByTime(FILTER_FRAMES_MS * 2)
    expect(onStall).not.toHaveBeenCalled()
  })

  it('ends with the signal', () => {
    const onStall = vi.fn()
    const ctx = context()
    ctx.run()
    const controller = new AbortController()
    watchFilterFrames(asContext(ctx), {}, controller.signal, onStall)
    controller.abort()
    vi.advanceTimersByTime(FILTER_FRAMES_MS * 2)
    expect(onStall).not.toHaveBeenCalled()
  })
})

describe('leaving the page', () => {
  it('runs the callback for a real leave only, not for the back/forward cache', () => {
    const callback = vi.fn()
    const off = onPageLeave(callback)
    pageEvent('pagehide', true)
    expect(callback).not.toHaveBeenCalled()
    expect(isPageUnloading()).toBe(false)
    pageEvent('pagehide', false)
    expect(callback).toHaveBeenCalledOnce()
    off()
    pageEvent('pagehide', false)
    expect(callback).toHaveBeenCalledOnce()
  })
})
