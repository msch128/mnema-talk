import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { computed, defineComponent, h, nextTick, ref } from 'vue'
import type { Channel, Category } from '../types/domain'
import type { ChannelTree } from '../lib/channelTree'
import { channelFixture, categoryFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import { setLocale } from '../i18n'
import { PEEK_MS, useSidebarReorder } from './useSidebarReorder'
import type { useChannelLayout } from './useChannelLayout'

type Tree = ChannelTree<Channel, Category>
class DragPointer extends MouseEvent implements PointerEvent {
  width = 1; height = 1; pressure = 0; tangentialPressure = 0; tiltX = 0; tiltY = 0; twist = 0; altitudeAngle = 0; azimuthAngle = 0; persistentDeviceId = 0
  pointerId = 1; pointerType = 'mouse'; isPrimary = true
  getCoalescedEvents(): PointerEvent[] { return [] }
  getPredictedEvents(): PointerEvent[] { return [] }
}
function pointer(type: string, x: number, y: number, touch = false) {
  const e = new DragPointer(type, { clientX: x, clientY: y, bubbles: true, cancelable: true })
  if (touch) e.pointerType = 'touch'
  return e
}
const u1 = channelFixture({ id: 'u1', name: 'Free' })
const a1 = channelFixture({ id: 'a1', name: 'Chat', category_id: 'A' })
const a2 = channelFixture({ id: 'a2', name: 'Call', category_id: 'A', type: 'voice' })
const b1 = channelFixture({ id: 'b1', category_id: 'B' })
const A = categoryFixture({ id: 'A', name: 'Alpha', channels: [a1, a2] })
const B = categoryFixture({ id: 'B', name: 'Beta', channels: [b1] })
const C = categoryFixture({ id: 'C', name: 'Empty' })
const wrappers: (() => void)[] = []
function setup({ initiallyCollapsed = false, noNav = false } = {}) {
  const tree = ref<Tree>({ uncategorized: [u1], categories: [A, B, C] })
  const enabled = ref(true)
  const collapsed = ref(new Set(initiallyCollapsed ? ['B'] : []))
  const expand = vi.fn((id: string) => collapsed.value.delete(id))
  const commit = vi.fn<ReturnType<typeof useChannelLayout>['commit']>(async next => {
    tree.value = { uncategorized: next.uncategorized, categories: next.categories.map(category => categoryFixture({ ...category, channels: category.channels ?? [] })) }
    return true
  })
  const longPress = vi.fn()
  let reorder: ReturnType<typeof useSidebarReorder> | undefined
  const wrapper = mount(defineComponent({
    setup() {
      reorder = useSidebarReorder({ layout: computed(() => tree.value), commit, collapsed, expandCategory: expand, enabled: () => enabled.value, onLongPress: longPress })
      return () => h('nav')
    }
  }), { attachTo: document.body })
  wrappers.push(() => wrapper.unmount())
  const api = required(reorder)
  const nav = wrapper.element
  if (!(nav instanceof HTMLElement)) throw new Error('Expected nav')
  nav.getBoundingClientRect = () => new DOMRect(0, 0, 200, 500)
  for (const [id, top, height, rows] of [
    ['__uncategorized', 10, 30, [['channel', 'u1', 10, 20]]],
    ['A', 50, 110, [['header', 'A', 50, 20], ['channel', 'a1', 80, 20], ['channel', 'a2', 110, 20], ['channel-tail', 'a2', 130, 10]]],
    ['B', 190, 90, [['header', 'B', 190, 20], ['channel', 'b1', 220, 20]]],
    ['C', 300, 60, [['header', 'C', 300, 20], ['empty', 'C', 320, 20]]]
  ] satisfies [string, number, number, [string, string, number, number][]][]) {
    const section = document.createElement('section')
    section.dataset.dropSection = id
    section.getBoundingClientRect = () => new DOMRect(0, top, 200, height)
    nav.append(section)
    for (const [zone, itemId, rowTop, rowHeight] of rows) {
      const row = document.createElement('div')
      row.dataset.drop = zone
      row.dataset.id = itemId
      if (zone === 'header') row.dataset.categoryToggle = itemId
      row.tabIndex = 0
      row.getBoundingClientRect = () => new DOMRect(0, rowTop, 200, rowHeight)
      row.scrollIntoView = vi.fn()
      section.append(row)
    }
  }
  if (!noNav) api.navEl.value = nav
  function start(entity: Channel | Category = a1, touch = false) {
    const e = pointer('pointerdown', 100, 90, touch)
    api.startDrag(e, 'type' in entity ? 'channel' : 'category', entity)
  }
  function move(y: number, x = 100, touch = false) { window.dispatchEvent(pointer('pointermove', x, y, touch)) }
  function drop(y: number, x = 100, touch = false) { window.dispatchEvent(pointer('pointerup', x, y, touch)) }
  return { api, tree, enabled, collapsed, expand, commit, longPress, nav, start, move, drop, wrapper }
}
beforeEach(() => { vi.useFakeTimers(); setLocale('en') })
afterEach(() => { wrappers.splice(0).reverse().forEach(unmount => unmount()); vi.useRealTimers(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

describe('sidebar keyboard and reveal', () => {
  it('moves a channel into the uncategorized list and handles an empty category label', async () => {
    const { api, tree } = setup()
    api.moveByKey('channel', a1, -1)
    await nextTick()
    expect(tree.value.uncategorized.some(channel => channel.id === 'a1')).toBe(true)
    tree.value.categories = tree.value.categories.map(category => ({ ...category, name: '' }))
    api.moveByKey('channel', a1, 1)
    await nextTick()
    expect(api.announcement.value).toContain('Chat')
  })
  it('moves channels between categories, refocuses them and announces/highlights the new position', async () => {
    const { api, tree, expand, commit, nav } = setup()
    api.moveByKey('channel', a2, 1)
    await nextTick()
    expect(tree.value.categories[1]?.channels[0]?.id).toBe('a2')
    expect(expand).toHaveBeenCalledWith('B')
    expect(commit).toHaveBeenCalledOnce()
    expect(document.activeElement).toBe(nav.querySelector('[data-id="a2"]'))
    expect(api.announcement.value).toContain('Call')
    expect(api.flashKey.value).toBe('channel:a2')
    vi.advanceTimersByTime(1200)
    expect(api.flashKey.value).toBe('')
  })

  it('moves categories and announces top/bottom boundaries without saving', async () => {
    const { api, tree, commit } = setup()
    api.moveByKey('category', A, -1)
    await nextTick()
    expect(api.announcement.value).toContain('top')
    api.moveByKey('category', C, 1)
    await nextTick()
    expect(api.announcement.value).toContain('bottom')
    expect(commit).not.toHaveBeenCalled()
    api.moveByKey('category', B, -1)
    await nextTick()
    expect(tree.value.categories[0]?.id).toBe('B')
    expect(api.flashKey.value).toBe('category:B')
    expect(api.announcement.value).toContain('Beta')
  })

  it('reveals categorized and uncategorized channels, missing items and categories with motion preference', async () => {
    const { api, expand, nav } = setup()
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    api.reveal('channel', a1.id)
    await nextTick()
    expect(expand).toHaveBeenCalledWith('A')
    const row = required(nav.querySelector<HTMLElement>('[data-id="a1"]'))
    expect(row.scrollIntoView).toHaveBeenCalledWith({ block: 'nearest', behavior: 'auto' })
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    api.reveal('channel', u1.id)
    api.reveal('category', B.id)
    api.reveal('channel', 'missing')
    await nextTick()
    expect(api.flashKey.value).toBe('channel:missing')
    expect(expand).toHaveBeenCalledOnce()
    api.navEl.value = null
    api.reveal('category', B.id)
    await nextTick()
  })

  it('keeps member scrolling passive and ignores moves while disabled or already dragging', async () => {
    const { api, enabled, start, move, commit } = setup()
    expect(api.navListeners.value.touchmove).toBe(api.drag.touchMove)
    enabled.value = false
    await nextTick()
    expect(api.navListeners.value).toEqual({})
    api.moveByKey('channel', a1, 1)
    start()
    expect(api.drag.state.phase).toBe('idle')
    enabled.value = true
    await nextTick()
    start()
    move(140)
    api.moveByKey('channel', a1, 1)
    expect(commit).not.toHaveBeenCalled()
    enabled.value = false
    await nextTick()
    expect(api.drag.state.phase).toBe('idle')
  })
})

describe('sidebar pointer drop', () => {
  it('resolves empty sections, malformed drop markers and zero-height rows without crashing', async () => {
    const { nav, start, move, drop, api } = setup()
    const section = required(nav.querySelector('[data-drop-section="C"]'))
    section.innerHTML = ''
    start(); move(325); drop(325)
    const row = required(nav.querySelector<HTMLElement>('[data-id="b1"]'))
    row.dataset.drop = 'unsupported'
    start(u1); move(230); drop(230)
    row.dataset.drop = 'channel'
    row.getBoundingClientRect = () => new DOMRect(0, 220, 200, 0)
    start(a2); move(220)
    await nextTick()
    expect(api.drag.state.target).not.toBeNull()
    drop(220)
  })

  it('uses exported reorder helpers without an owning scope', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const standalone = useSidebarReorder({
      layout: computed(() => ({ uncategorized: [u1], categories: [A] })),
      commit: vi.fn(async () => true), collapsed: ref(new Set()), expandCategory: vi.fn(), enabled: () => false
    })
    expect(standalone.navListeners.value).toEqual({})
    expect(standalone.indicatorFor('unknown')).toBe('')
    warn.mockRestore()
  })
  it('preserves voice-channel type for the drag ghost', () => {
    const { api, start, move } = setup()
    start(a2)
    move(140)
    expect(api.dragItem.value).toMatchObject({ kind: 'channel', id: 'a2', type: 'voice' })
    expect(api.isDragged('channel', 'a2')).toBe(true)
    expect(api.isDragged('category', 'a2')).toBe(false)
  })

  it.each([[-1, 100], [501, 100], [100, -1], [100, 201]])('cancels dropping outside nav at y=%i,x=%i', (y, x) => {
    const { api, start, move, drop, commit } = setup()
    start(); move(y, x); drop(y, x)
    expect(api.drag.state.phase).toBe('idle')
    expect(commit).not.toHaveBeenCalled()
  })

  it('cancels drops without a mounted nav', () => {
    const { start, move, drop, commit } = setup({ noNav: true })
    start(); move(130); drop(130)
    expect(commit).not.toHaveBeenCalled()
  })

  it.each([5, 35, 45, 55, 75, 85, 125, 135, 170, 195, 250, 290, 325, 400])('resolves channel rows, gaps, tails and empty sections at y=%i', async y => {
    const { api, tree, start, move, drop } = setup()
    start(); move(y)
    await nextTick()
    const target = api.drag.state.target
    if (target?.indicator) expect(api.indicatorFor(target.indicator.key)).toBe(target.indicator.edge)
    expect(api.indicatorFor('missing')).toBe('')
    drop(y)
    expect(tree.value.categories.flatMap(cat => cat.channels).concat(tree.value.uncategorized).filter(channel => channel.id === 'a1')).toHaveLength(1)
  })

  it.each([5, 55, 170, 200, 290, 400])('moves categories by section bounds at y=%i', y => {
    const { api, tree, start, move, drop } = setup()
    start(B); move(y); drop(y)
    expect(api.drag.state.phase).toBe('idle')
    expect(tree.value.categories.map(category => category.id).sort()).toEqual(['A', 'B', 'C'])
  })

  it('opens a collapsed category only after hover delay and retains it after dropping', async () => {
    const { api, expand, start, move, drop } = setup({ initiallyCollapsed: true })
    expect(api.isCollapsed('B')).toBe(true)
    start(); move(200)
    await nextTick()
    vi.advanceTimersByTime(PEEK_MS - 1)
    expect(api.isCollapsed('B')).toBe(true)
    vi.advanceTimersByTime(1)
    await nextTick()
    expect(api.isCollapsed('B')).toBe(false)
    drop(200)
    expect(expand).toHaveBeenCalledWith('B')
  })

  it('cancels hover peeks when leaving the header or ending the drag', async () => {
    const { api, start, move } = setup({ initiallyCollapsed: true })
    start(); move(200)
    await nextTick()
    move(290)
    await nextTick()
    vi.advanceTimersByTime(PEEK_MS)
    expect(api.isCollapsed('B')).toBe(true)
    move(200)
    await nextTick()
    vi.advanceTimersByTime(PEEK_MS)
    await nextTick()
    expect(api.isCollapsed('B')).toBe(false)
    api.drag.cancel()
    expect(api.isCollapsed('B')).toBe(true)
  })

  it.each([a1, B])('opens touch long-press actions for $id', entity => {
    const { start, drop, longPress } = setup()
    start(entity, true)
    vi.advanceTimersByTime(350)
    drop(90, 100, true)
    expect(longPress).toHaveBeenCalledWith('type' in entity ? 'channel' : 'category', entity, expect.any(MouseEvent))
  })

  it('tolerates a row deleted during a long press', () => {
    const { tree, start, drop, longPress } = setup()
    start(a1, true)
    tree.value.categories = []
    vi.advanceTimersByTime(350)
    drop(90, 100, true)
    expect(longPress).not.toHaveBeenCalled()
  })

  it('tolerates a category deleted during a long press', () => {
    const { tree, start, drop, longPress } = setup()
    start(B, true)
    tree.value.categories = []
    vi.advanceTimersByTime(350)
    drop(90, 100, true)
    expect(longPress).not.toHaveBeenCalled()
  })
})
