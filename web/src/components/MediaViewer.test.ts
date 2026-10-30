import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import MediaViewer from './MediaViewer.vue'
import { closeViewer, openViewer, viewerMessageFrom, viewerState, type ViewerItem } from '../composables/useMediaViewer'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale, t } from '../i18n'

const items: ViewerItem[] = [
  { src: '/m/a', name: 'a.png', sizeBytes: 2048, mimeType: 'image/png', width: 800, height: 600 },
  { src: '/m/b', name: 'b.png', sizeBytes: 1048576, mimeType: 'image/png' },
  { src: '/m/c', name: 'c.png', mimeType: 'image/png' }
]
const message = { id: 'm1', channelId: 'c1', channelName: 'lobby', senderName: 'Ada', senderAvatarUrl: '/avatar.png', sentAt: '2026-01-01T10:00:00Z' }

let w: ReturnType<typeof mount<typeof MediaViewer>>
// The viewer is mounted once for the whole app and opened later: so is it here.
async function show(req: Parameters<typeof openViewer>[0]) {
  w = mount(MediaViewer, { attachTo: document.body })
  openViewer(req)
  await nextTick(); await nextTick()
}
const tid = (id: string) => w.get(`[data-testid="${id}"]`)
const key = async (k: string) => { document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })); await nextTick() }
const press = async (k: string) => { await tid('media-viewer').trigger('keydown', { key: k }) }

beforeEach(() => { setLocale('en'); setActivePinia(createPinia()) })
afterEach(() => { w?.unmount(); closeViewer(); document.body.innerHTML = ''; vi.restoreAllMocks() })

describe('MediaViewer', () => {
  it('is a labelled modal dialog and renders nothing while closed', async () => {
    w = mount(MediaViewer, { attachTo: document.body })
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    w.unmount()
    await show({ items, index: 0, message })
    const dialog = w.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('aria-label')).toBe(t('viewer.label'))
    expect(document.activeElement).toBe(tid('viewer-close').element)
  })

  it('navigates with arrow keys and buttons, counts, and stops at the ends', async () => {
    await show({ items, index: 0 })
    expect(tid('viewer-counter').text()).toBe('1 / 3')
    expect(tid('viewer-prev').attributes('disabled')).toBeDefined()
    await press('ArrowRight')
    expect(tid('viewer-counter').text()).toBe('2 / 3')
    expect(tid('viewer-image').attributes('src')).toBe('/m/b')
    await tid('viewer-next').trigger('click')
    expect(tid('viewer-counter').text()).toBe('3 / 3')
    expect(tid('viewer-next').attributes('disabled')).toBeDefined()
    await press('ArrowRight')
    expect(tid('viewer-counter').text()).toBe('3 / 3')
    await tid('viewer-prev').trigger('click')
    await press('ArrowLeft')
    expect(tid('viewer-counter').text()).toBe('1 / 3')
  })

  it('announces the current file in a live region on navigation', async () => {
    await show({ items, index: 0 })
    expect(tid('viewer-live').attributes('aria-live')).toBe('polite')
    await press('ArrowRight')
    expect(tid('viewer-live').text()).toBe(t('viewer.announce', { name: 'b.png', n: 2, total: 3 }))
  })

  it('closes on Escape, the close button and a backdrop click, but not on control or image clicks', async () => {
    await show({ items, index: 0, message })
    await tid('viewer-image').trigger('click')
    await tid('viewer-original').trigger('click')
    await tid('viewer-next').trigger('click')
    expect(viewerState.open).toBe(true)
    await tid('viewer-stage').trigger('click')
    expect(viewerState.open).toBe(false)
    openViewer({ items, index: 0 }); await nextTick()
    await key('Escape')
    expect(viewerState.open).toBe(false)
    openViewer({ items, index: 0 }); await nextTick()
    await tid('viewer-close').trigger('click')
    expect(viewerState.open).toBe(false)
  })

  it('shows file name, sender, time, channel, size and dimensions', async () => {
    await show({ items, index: 0, message })
    expect(tid('viewer-name').text()).toBe('a.png')
    const sender = tid('viewer-sender').text()
    expect(sender).toContain('Ada')
    expect(sender).toContain(t('viewer.inChannel', { channel: 'lobby' }))
    expect(sender).toMatch(/\d/)
    expect(tid('viewer-details').text()).toBe('2 KB · 800 × 600 px')
  })

  it('reads pixel dimensions from the loaded image when the item has none', async () => {
    await show({ items, index: 1, message })
    expect(tid('viewer-details').text()).toBe('1 MB')
    const img = tid('viewer-image').element as HTMLImageElement
    Object.defineProperty(img, 'naturalWidth', { value: 1920 })
    Object.defineProperty(img, 'naturalHeight', { value: 1080 })
    await tid('viewer-image').trigger('load')
    expect(tid('viewer-details').text()).toBe('1 MB · 1920 × 1080 px')
  })

  it('offers open original, download and copy link', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    await show({ items, index: 0, message })
    expect(tid('viewer-original').attributes()).toMatchObject({ href: '/m/a', target: '_blank', rel: 'noopener noreferrer' })
    expect(tid('viewer-download').attributes()).toMatchObject({ href: '/m/a', download: 'a.png' })
    await tid('viewer-copy').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(new URL('/m/a', window.location.href).href)
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('viewer.linkCopied'))
    vi.unstubAllGlobals()
  })

  it('jumps to the message and closes', async () => {
    const jump = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    await show({ items, index: 0, message })
    await tid('viewer-jump').trigger('click')
    expect(jump).toHaveBeenCalledWith({ id: 'm1', channel_id: 'c1' })
    expect(viewerState.open).toBe(false)
  })

  it('degrades without message details: no sender, no jump, still usable', async () => {
    await show({ items: [items[2]!], index: 0 })
    expect(w.find('[data-testid="viewer-sender"]').exists()).toBe(false)
    expect(w.find('[data-testid="viewer-jump"]').exists()).toBe(false)
    expect(w.find('[data-testid="viewer-details"]').exists()).toBe(false)
    expect(w.find('[data-testid="viewer-counter"]').exists()).toBe(false)
    expect(w.find('[data-testid="viewer-prev"]').exists()).toBe(false)
    expect(tid('viewer-name').text()).toBe('c.png')
    expect(w.find('[data-testid="viewer-download"]').exists()).toBe(true)
  })

  it('omits only the channel when the channel name is unknown', async () => {
    await show({ items, index: 0, message: { id: 'm1', channelId: 'c1', senderName: 'Ada', sentAt: message.sentAt } })
    expect(tid('viewer-sender').text()).not.toContain(t('viewer.inChannel', { channel: '' }).trim())
    expect(w.find('[data-testid="viewer-jump"]').exists()).toBe(true)
  })

  it('zooms with buttons and keys, resets with 0 and on navigation', async () => {
    await show({ items, index: 0 })
    const scale = () => tid('viewer-image').attributes('style')
    expect(scale()).toContain('scale(1)')
    await tid('viewer-zoom-in').trigger('click')
    expect(scale()).toContain('scale(1.5)')
    await press('+')
    expect(scale()).toContain('scale(2.25)')
    await press('-')
    expect(scale()).toContain('scale(1.5)')
    await press('0')
    expect(scale()).toContain('scale(1)')
    await press('=')
    await press('ArrowRight')
    expect(scale()).toContain('scale(1)')
    await tid('viewer-zoom-in').trigger('click')
    await tid('viewer-zoom-reset').trigger('click')
    expect(scale()).toContain('scale(1)')
  })

  it('zooms on double click and wheel, and pans a zoomed image by dragging', async () => {
    await show({ items, index: 0 })
    const img = tid('viewer-image')
    await img.trigger('dblclick', { clientX: 0, clientY: 0 })
    expect(img.attributes('style')).toContain('scale(2.5)')
    const before = img.attributes('style')
    const ev = (type: string, x: number, y: number) => new PointerEvent(type, { bubbles: true, clientX: x, clientY: y, pointerId: 1, button: 0 })
    img.element.dispatchEvent(ev('pointerdown', 10, 10))
    tid('viewer-stage').element.dispatchEvent(ev('pointermove', 40, 30))
    tid('viewer-stage').element.dispatchEvent(ev('pointerup', 40, 30))
    await nextTick()
    expect(img.attributes('style')).not.toBe(before)
    expect(img.attributes('style')).toContain('translate3d(30px, 20px, 0)')
    await img.trigger('dblclick')
    expect(img.attributes('style')).toContain('scale(1)')
    await tid('viewer-stage').trigger('wheel', { deltaY: -200 })
    expect(img.attributes('style')).not.toContain('scale(1)')
  })

  it('jumps to a thread reply through its thread', async () => {
    const jump = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    await show({ items, index: 0, message: { ...message, id: 'reply1', parentId: 'root1' } })
    await tid('viewer-jump').trigger('click')
    expect(jump).toHaveBeenCalledWith({ id: 'reply1', channel_id: 'c1', parent_id: 'root1' })
  })

  it('keeps a click on a zoomed picture, and a double-click still resets', async () => {
    await show({ items, index: 0 })
    await tid('viewer-image').trigger('dblclick')
    const capture = vi.fn()
    ;(tid('viewer-stage').element as HTMLElement).setPointerCapture = capture
    const ev = (type: string) => new PointerEvent(type, { bubbles: true, clientX: 10, pointerId: 1, button: 0 })
    tid('viewer-image').element.dispatchEvent(ev('pointerdown'))
    tid('viewer-image').element.dispatchEvent(ev('pointerup'))
    await tid('viewer-image').trigger('click')
    expect(capture).not.toHaveBeenCalled()
    expect(viewerState.open).toBe(true)
    await tid('viewer-image').trigger('dblclick')
    expect(tid('viewer-image').attributes('style')).toContain('scale(1)')
  })

  it('captures the pointer only once a drag starts', async () => {
    await show({ items, index: 0 })
    await tid('viewer-image').trigger('dblclick')
    const capture = vi.fn()
    ;(tid('viewer-stage').element as HTMLElement).setPointerCapture = capture
    const ev = (type: string, x: number) => new PointerEvent(type, { bubbles: true, clientX: x, pointerId: 1, button: 0 })
    tid('viewer-image').element.dispatchEvent(ev('pointerdown', 0))
    expect(capture).not.toHaveBeenCalled()
    tid('viewer-stage').element.dispatchEvent(ev('pointermove', 30))
    expect(capture).toHaveBeenCalledWith(1)
  })

  it('keeps the zoom-out button usable (a no-op) at the smallest size', async () => {
    await show({ items, index: 0 })
    expect(tid('viewer-zoom-out').attributes('disabled')).toBeUndefined()
  })

  it('does not close when a drag ends on the backdrop', async () => {
    await show({ items, index: 0 })
    await tid('viewer-image').trigger('dblclick')
    const ev = (type: string, x: number) => new PointerEvent(type, { bubbles: true, clientX: x, pointerId: 1, button: 0 })
    tid('viewer-image').element.dispatchEvent(ev('pointerdown', 0))
    tid('viewer-stage').element.dispatchEvent(ev('pointermove', 50))
    tid('viewer-stage').element.dispatchEvent(ev('pointerup', 50))
    await tid('viewer-stage').trigger('click')
    expect(viewerState.open).toBe(true)
  })
})

describe('viewerMessageFrom', () => {
  it('carries the details a viewer shows, and only those that exist', () => {
    const base = { id: 'm9', channel_id: 'c9', created_at: '2026-01-01T10:00:00Z', username: 'ada', display_name: 'Ada' } as Parameters<typeof viewerMessageFrom>[0]
    expect(viewerMessageFrom(base)).toMatchObject({ id: 'm9', channelId: 'c9', senderName: 'Ada' })
    expect(viewerMessageFrom(base)).not.toHaveProperty('parentId')
    expect(viewerMessageFrom({ ...base, parent_id: 'root', avatar_url: '/a.png' }, 'lobby')).toMatchObject({ parentId: 'root', senderAvatarUrl: '/a.png', channelName: 'lobby' })
  })
})

describe('useMediaViewer', () => {
  it('clamps the index and ignores an empty list', () => {
    openViewer({ items: [], index: 0 })
    expect(viewerState.open).toBe(false)
    openViewer({ items, index: 99 })
    expect(viewerState.index).toBe(2)
    closeViewer()
    expect(viewerState.items).toEqual([])
  })
})
