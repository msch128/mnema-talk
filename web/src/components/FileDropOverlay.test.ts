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
})
