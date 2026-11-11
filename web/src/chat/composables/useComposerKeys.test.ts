import { describe, it, expect, vi, afterEach } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { handleComposerKeydown, useEscapeMarksRead } from './useComposerKeys'

function key(k: string, init: KeyboardEventInit = {}) {
  return new KeyboardEvent('keydown', { key: k, cancelable: true, bubbles: true, ...init })
}

function keys(over: Partial<Parameters<typeof handleComposerKeydown>[1]> = {}) {
  return { consumers: [], replying: () => false, cancelReply: vi.fn(), send: vi.fn(), ...over }
}

describe('composer keys', () => {
  it('sends on Enter and keeps Shift+Enter for the line break', () => {
    const k = keys()
    const enter = key('Enter')
    handleComposerKeydown(enter, k)
    expect(enter.defaultPrevented).toBe(true)
    expect(k.send).toHaveBeenCalledTimes(1)
    const shifted = key('Enter', { shiftKey: true })
    handleComposerKeydown(shifted, k)
    expect(shifted.defaultPrevented).toBe(false)
    expect(k.send).toHaveBeenCalledTimes(1)
  })

  it('leaves the Enter that confirms an IME composition to the IME', () => {
    const k = keys()
    for (const init of [{ isComposing: true }, { keyCode: 229 }]) {
      const enter = key('Enter', init)
      handleComposerKeydown(enter, k)
      expect(enter.defaultPrevented).toBe(false)
    }
    expect(k.send).not.toHaveBeenCalled()
  })

  it('leaves a reply on Escape, and only while replying', () => {
    const k = keys({ replying: () => true })
    const esc = key('Escape')
    handleComposerKeydown(esc, k)
    expect(esc.defaultPrevented).toBe(true)
    expect(k.cancelReply).toHaveBeenCalledTimes(1)
    const idle = keys()
    handleComposerKeydown(key('Escape'), idle)
    expect(idle.cancelReply).not.toHaveBeenCalled()
  })

  it('lets a suggestion list take the key first', () => {
    const k = keys({ consumers: [() => false, () => true] })
    const enter = key('Enter')
    handleComposerKeydown(enter, k)
    expect(k.send).not.toHaveBeenCalled()
    expect(enter.defaultPrevented).toBe(false)
  })
})

describe('Escape marks the channel read', () => {
  afterEach(() => { document.body.replaceChildren() })

  function mountEscape(over: Partial<Parameters<typeof useEscapeMarksRead>[0]> = {}) {
    const composer = ref<HTMLTextAreaElement | null>(null)
    const markAllRead = vi.fn(() => true)
    const closeMenu = vi.fn(() => false)
    const wrapper = mount(defineComponent({
      setup() {
        useEscapeMarksRead({ composer, closeMenu, occupied: () => false, draft: () => '', markAllRead, ...over })
        return () => h('div')
      }
    }), { attachTo: document.body })
    return { wrapper, composer, markAllRead, closeMenu }
  }

  it('marks read on a bare Escape and consumes it', () => {
    const { wrapper, markAllRead } = mountEscape()
    const esc = key('Escape')
    document.body.dispatchEvent(esc)
    expect(markAllRead).toHaveBeenCalledTimes(1)
    expect(esc.defaultPrevented).toBe(true)
    wrapper.unmount()
    document.body.dispatchEvent(key('Escape'))
    expect(markAllRead).toHaveBeenCalledTimes(1)
  })

  it('ignores other keys', () => {
    const { wrapper, markAllRead } = mountEscape()
    document.body.dispatchEvent(key('a'))
    expect(markAllRead).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('closes an open menu instead', () => {
    const { wrapper, markAllRead } = mountEscape({ closeMenu: () => true })
    document.body.dispatchEvent(key('Escape'))
    expect(markAllRead).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('leaves the key to whatever already used it', () => {
    const taken = mountEscape({ occupied: () => true })
    document.body.dispatchEvent(key('Escape'))
    expect(taken.markAllRead).not.toHaveBeenCalled()
    taken.wrapper.unmount()
    const typed = mountEscape({ draft: () => ' text ' })
    document.body.dispatchEvent(key('Escape'))
    expect(typed.markAllRead).not.toHaveBeenCalled()
    typed.wrapper.unmount()
    const dialog = mountEscape()
    const modal = document.createElement('div')
    modal.setAttribute('role', 'dialog')
    document.body.append(modal)
    document.body.dispatchEvent(key('Escape'))
    expect(dialog.markAllRead).not.toHaveBeenCalled()
    dialog.wrapper.unmount()
  })

  it('does not fire from another text field, but does from the composer', () => {
    const { wrapper, composer, markAllRead } = mountEscape()
    const other = document.createElement('input')
    const own = document.createElement('textarea')
    document.body.append(other, own)
    composer.value = own
    other.dispatchEvent(key('Escape'))
    expect(markAllRead).not.toHaveBeenCalled()
    own.dispatchEvent(key('Escape'))
    expect(markAllRead).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
