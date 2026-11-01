import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useVoiceStore } from '../stores/voice'
import { useVisibleCameras } from './useVisibleCameras'

// A stand-in that lets the test say which tiles are on screen.
class FakeObserver {
  static all: FakeObserver[] = []
  observed = new Set<Element>()
  disconnected = false
  constructor(private readonly callback: IntersectionObserverCallback) { FakeObserver.all.push(this) }
  observe(el: Element) { this.observed.add(el) }
  unobserve(el: Element) { this.observed.delete(el) }
  disconnect() { this.disconnected = true }
  show(...els: Element[]) { this.report(els, true) }
  hide(...els: Element[]) { this.report(els, false) }
  private report(els: Element[], isIntersecting: boolean) {
    this.callback(els.map(target => ({ target, isIntersecting }) as IntersectionObserverEntry), this as unknown as IntersectionObserver)
  }
}

const users = ref<string[]>([])
const View = defineComponent({
  setup() {
    const root = ref<HTMLElement | null>(null)
    useVisibleCameras(root)
    return () => h('main', { ref: root }, users.value.map(id => h('div', { 'data-participant-tile': '', 'data-user-id': id, key: id })))
  }
})

const tile = (id: string, w: ReturnType<typeof mount>) => w.get(`[data-user-id="${id}"]`).element

beforeEach(() => {
  setActivePinia(createPinia())
  FakeObserver.all = []
  users.value = ['a', 'b']
  vi.stubGlobal('IntersectionObserver', FakeObserver)
})
afterEach(() => vi.unstubAllGlobals())

describe('useVisibleCameras', () => {
  it('reports the tiles that are on screen, as they come and go', async () => {
    const voice = useVoiceStore()
    const spy = vi.spyOn(voice, 'setVisibleCameras')
    const w = mount(View, { attachTo: document.body })
    const [observer] = FakeObserver.all
    if (!observer) throw new Error('no observer')
    expect([...observer.observed]).toHaveLength(2)
    expect(spy).toHaveBeenLastCalledWith([])

    observer.show(tile('a', w))
    expect(spy).toHaveBeenLastCalledWith(['a'])
    observer.show(tile('b', w))
    expect(spy).toHaveBeenLastCalledWith(['a', 'b'])
    observer.hide(tile('a', w))
    expect(spy).toHaveBeenLastCalledWith(['b'])

    users.value = ['b', 'c']
    await nextTick()
    await Promise.resolve()
    expect([...observer.observed]).toHaveLength(2)
    observer.show(tile('c', w))
    expect(spy).toHaveBeenLastCalledWith(['b', 'c'])

    users.value = ['c']
    await nextTick()
    await Promise.resolve()
    expect(spy).toHaveBeenLastCalledWith(['c'])
    w.unmount()
  })

  it('reports nobody when the view goes away during a call, and stops watching', async () => {
    const voice = useVoiceStore()
    voice.isConnected = true
    const spy = vi.spyOn(voice, 'setVisibleCameras')
    const w = mount(View, { attachTo: document.body })
    w.unmount()
    expect(spy).toHaveBeenLastCalledWith([])
    expect(FakeObserver.all[0]?.disconnected).toBe(true)
  })

  it('goes back to receiving every camera when the view goes away outside a call', () => {
    const voice = useVoiceStore()
    const spy = vi.spyOn(voice, 'setVisibleCameras')
    const w = mount(View, { attachTo: document.body })
    w.unmount()
    expect(spy).toHaveBeenLastCalledWith(null)
  })

  it('does nothing in a browser without IntersectionObserver', () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    const voice = useVoiceStore()
    const spy = vi.spyOn(voice, 'setVisibleCameras')
    const w = mount(View, { attachTo: document.body })
    expect(spy).not.toHaveBeenCalled()
    w.unmount()
    expect(spy).toHaveBeenLastCalledWith(null)
  })
})
