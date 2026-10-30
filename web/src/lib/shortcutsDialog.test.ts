import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h } from 'vue'
import { isShortcutsKey, shortcutsOpen, useShortcutsKey } from './shortcutsDialog'

function key(init: KeyboardEventInit, target: EventTarget = window) {
  const e = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
  target.dispatchEvent(e)
  return e
}

describe('isShortcutsKey', () => {
  it('takes Ctrl or Cmd with the slash, also where the slash needs Shift or AltGr', () => {
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '/', ctrlKey: true }))).toBe(true)
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '/', metaKey: true }))).toBe(true)
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '/', ctrlKey: true, shiftKey: true }))).toBe(true)
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '/' }))).toBe(false)
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '/', ctrlKey: true, altKey: true }))).toBe(false)
  })

  it('takes a bare question mark outside text fields only', () => {
    const field = document.createElement('textarea')
    document.body.append(field)
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '?', shiftKey: true }))).toBe(true)
    const typed = key({ key: '?', shiftKey: true }, field)
    expect(isShortcutsKey(typed)).toBe(false)
    expect(isShortcutsKey(new KeyboardEvent('keydown', { key: '?', ctrlKey: true }))).toBe(false)
    field.remove()
  })
})

describe('useShortcutsKey', () => {
  let enabled = true
  let app: ReturnType<typeof createApp>
  beforeEach(() => {
    enabled = true
    shortcutsOpen.value = false
    app = createApp(defineComponent({ setup() { useShortcutsKey(() => enabled); return () => h('div') } }))
    app.mount(document.createElement('div'))
  })
  afterEach(() => {
    app.unmount()
    document.body.replaceChildren()
  })

  it('opens the list, and Ctrl+/ again closes it', () => {
    const e = key({ key: '/', ctrlKey: true })
    expect(e.defaultPrevented).toBe(true)
    expect(shortcutsOpen.value).toBe(true)
    key({ key: '/', ctrlKey: true })
    expect(shortcutsOpen.value).toBe(false)
  })

  it('opens on a question mark and keeps it open on another one', () => {
    key({ key: '?', shiftKey: true })
    expect(shortcutsOpen.value).toBe(true)
    key({ key: '?', shiftKey: true })
    expect(shortcutsOpen.value).toBe(true)
  })

  it('leaves a question mark alone while another dialog is open', () => {
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    document.body.append(dialog)
    key({ key: '?', shiftKey: true })
    expect(shortcutsOpen.value).toBe(false)
  })

  it('does nothing before sign-in', () => {
    enabled = false
    key({ key: '/', ctrlKey: true })
    expect(shortcutsOpen.value).toBe(false)
  })
})
