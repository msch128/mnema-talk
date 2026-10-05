import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope, ref, nextTick } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageActions, formatTime } from './useMessageActions'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../lib/confirm'
import { setLocale } from '../i18n'

let scope

function setup(options) {
  scope = effectScope()
  return scope.run(() => useMessageActions(options))
}

// A file input stand-in: browsers only allow '' as a value to set.
function fileInput(file = new File(['x'], 'a.png', { type: 'image/png' })) {
  return { files: [file], value: 'C:\\fakepath\\a.png' }
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = { id: 'me', username: 'me', role: 'user' }
  useChatStore().activeChannel = { id: 'ch1', name: 'general', type: 'text' }
})

afterEach(() => {
  scope?.stop()
  pendingConfirm.value = null
  document.body.innerHTML = ''
})

describe('useMessageActions upload', () => {
  it('clears the file input after a failed upload so the same file can be picked again', async () => {
    const a = setup()
    const input = fileInput()
    const ok = await a.upload(input, () => Promise.reject(new Error('too big')))
    expect(ok).toBe(false)
    expect(input.value).toBe('')
    expect(a.isUploading.value).toBe(false)
    expect(useToastStore().toasts.at(-1).text).toBe('too big')
  })

  it('clears the input after success too and reports success', async () => {
    const a = setup()
    const input = fileInput()
    const send = vi.fn(() => Promise.resolve())
    expect(await a.upload(input, send)).toBe(true)
    expect(send).toHaveBeenCalledWith(input.files[0])
    expect(input.value).toBe('')
  })

  it('does nothing without a file', async () => {
    const a = setup()
    const send = vi.fn()
    expect(await a.upload({ files: [], value: '' }, send)).toBe(false)
    expect(send).not.toHaveBeenCalled()
  })

  it('is uploading while the request runs', async () => {
    const a = setup()
    let finish
    const p = a.upload(fileInput(), () => new Promise(r => { finish = r }))
    expect(a.isUploading.value).toBe(true)
    finish()
    await p
    expect(a.isUploading.value).toBe(false)
  })
})

describe('useMessageActions edit / delete / react', () => {
  const msg = { id: 'm1', user_id: 'me', content: 'hello', channel_id: 'ch1' }

  it('edits through the store and leaves edit mode', async () => {
    const a = setup()
    const edit = vi.spyOn(useChatStore(), 'editMessage').mockResolvedValue()
    a.startEdit(msg)
    expect(a.editingId.value).toBe('m1')
    expect(a.editText.value).toBe('hello')
    a.editText.value = '  changed  '
    await a.saveEdit(msg)
    expect(edit).toHaveBeenCalledWith('ch1', 'm1', 'changed')
    expect(a.editingId.value).toBe(null)
  })

  it('keeps edit mode and shows the error when saving fails', async () => {
    const a = setup()
    vi.spyOn(useChatStore(), 'editMessage').mockRejectedValue(new Error('nope'))
    a.startEdit(msg)
    await a.saveEdit(msg)
    expect(a.editingId.value).toBe('m1')
    expect(useToastStore().toasts.at(-1).text).toBe('nope')
  })

  it('deletes only after confirmation, with the given title', async () => {
    const a = setup({ deleteTitle: 'thread.deleteTitle' })
    const del = vi.spyOn(useChatStore(), 'deleteMessage').mockResolvedValue()
    const p = a.deleteMessage(msg)
    expect(pendingConfirm.value.title).toBe('Delete reply?')
    pendingConfirm.value.resolve(true)
    await p
    expect(del).toHaveBeenCalledWith('ch1', 'm1')
  })

  it('knows own messages and who may delete', () => {
    const a = setup()
    expect(a.isOwn(msg)).toBe(true)
    expect(a.isOwn({ ...msg, user_id: 'x' })).toBe(false)
    expect(a.canDelete({ ...msg, user_id: 'x' })).toBe(false)
    useAuthStore().user = { id: 'me', username: 'me', role: 'admin' }
    expect(a.canDelete({ ...msg, user_id: 'x' })).toBe(true)
  })

  it('reacting closes the picker', async () => {
    const a = setup()
    const toggle = vi.spyOn(useChatStore(), 'toggleReaction').mockResolvedValue()
    a.togglePicker('m1')
    await a.toggleReaction('m1', '🔥')
    expect(toggle).toHaveBeenCalledWith('m1', '🔥')
    expect(a.pickerId.value).toBe(null)
  })
})

describe('useMessageActions reaction picker', () => {
  it('closes on a press outside the picker anchors and on Escape', async () => {
    const container = document.createElement('div')
    container.innerHTML = '<div class="reaction-picker-anchor"><button id="in"></button></div><p id="out"></p>'
    document.body.appendChild(container)
    const a = setup({ container: ref(container) })

    a.togglePicker('m1')
    await nextTick()
    container.querySelector('#in').dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(a.pickerId.value).toBe('m1')
    container.querySelector('#out').dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(a.pickerId.value).toBe(null)

    a.togglePicker('m1')
    await nextTick()
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(a.pickerId.value).toBe(null)
  })

  it('toggles the same picker closed again', () => {
    const a = setup()
    a.togglePicker('bottom-m1')
    expect(a.pickerId.value).toBe('bottom-m1')
    a.togglePicker('bottom-m1')
    expect(a.pickerId.value).toBe(null)
  })
})

describe('formatTime', () => {
  it('returns hours and minutes, empty for no date', () => {
    expect(formatTime('')).toBe('')
    expect(formatTime('2026-01-01T10:05:00Z')).toMatch(/\d{2}:\d{2}/)
  })
})
