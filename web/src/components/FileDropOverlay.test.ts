import { describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import FileDropOverlay from './FileDropOverlay.vue'

const png = new File(['x'], 'a.png', { type: 'image/png' })

function drag(type: string, types: string[], files: File[] = []) {
  const e = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(e, 'dataTransfer', { value: { types, files, dropEffect: 'none' } })
  return e
}

function setup() {
  const received: File[][] = []
  const w = mount(defineComponent({
    setup: () => () => h('main', { class: 'relative' }, [h('p', 'chat'), h(FileDropOverlay, { onFiles: (f: File[]) => received.push(f) })]),
    global: { directives: {} }
  }), { attachTo: document.body, global: { mocks: { $t: (k: string) => k } } })
  return { w, host: w.element as HTMLElement, received, overlay: () => w.get('[data-testid="file-drop-overlay"]') }
}

describe('FileDropOverlay', () => {
  it('shows while files are dragged over the chat and hides on leave', async () => {
    const { host, overlay } = setup()
    expect(overlay().isVisible()).toBe(false)
    host.dispatchEvent(drag('dragenter', ['Files']))
    await nextTick()
    expect(overlay().isVisible()).toBe(true)
    host.dispatchEvent(drag('dragleave', ['Files']))
    await nextTick()
    expect(overlay().isVisible()).toBe(false)
  })

  it('ignores drags of text', () => {
    const { host, overlay } = setup()
    host.dispatchEvent(drag('dragenter', ['text/plain']))
    expect(overlay().isVisible()).toBe(false)
  })

  it('hands the dropped files on and hides', async () => {
    const { host, overlay, received } = setup()
    host.dispatchEvent(drag('dragenter', ['Files']))
    const drop = drag('drop', ['Files'], [png])
    host.dispatchEvent(drop)
    expect(drop.defaultPrevented).toBe(true)
    await nextTick()
    expect(received).toEqual([[png]])
    expect(overlay().isVisible()).toBe(false)
  })

  it('allows the drop with a copy cursor, only for files', () => {
    const { host } = setup()
    const files = drag('dragover', ['Files'])
    host.dispatchEvent(files)
    expect(files.defaultPrevented).toBe(true)
    expect((files as unknown as { dataTransfer: { dropEffect: string } }).dataTransfer.dropEffect).toBe('copy')
    const text = drag('dragover', ['text/plain'])
    host.dispatchEvent(text)
    expect(text.defaultPrevented).toBe(false)
  })

  it('stays up while the drag moves between children and ignores text leaving or dropping', async () => {
    const { host, overlay } = setup()
    host.dispatchEvent(drag('dragenter', ['Files']))
    host.dispatchEvent(drag('dragenter', ['Files']))
    host.dispatchEvent(drag('dragleave', ['Files']))
    host.dispatchEvent(drag('dragleave', ['text/plain']))
    const text = drag('drop', ['text/plain'], [png])
    host.dispatchEvent(text)
    await nextTick()
    expect(text.defaultPrevented).toBe(false)
    expect(overlay().isVisible()).toBe(true)
  })

  it('stops listening when it is removed', () => {
    const { w, host, received } = setup()
    w.unmount()
    host.dispatchEvent(drag('drop', ['Files'], [png]))
    expect(received).toEqual([])
  })
})
