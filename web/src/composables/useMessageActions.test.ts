import { required } from '../store-test-support.fixture'
import type { EffectScope } from 'vue'
import { channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope, ref, nextTick } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageActions, formatTime } from './useMessageActions'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../lib/confirm'
import { setLocale } from '../i18n'

let scope: EffectScope | undefined

function setup(options: Parameters<typeof useMessageActions>[0] = {}) {
  scope = effectScope()
  return required(scope.run(() => useMessageActions(options)))
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me', username: 'me', role: 'user' })
  useChatStore().activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
})

afterEach(() => {
  scope?.stop()
  pendingConfirm.value = null
  document.body.innerHTML = ''
})

describe('useMessageActions edit / delete / react', () => {
  const msg = messageFixture({ id: 'm1', user_id: 'me', content: 'hello', channel_id: 'ch1' })

  it('keeps a newer edit open when the save of an older one finishes', async () => {
    const a = setup()
    const other = messageFixture({ id: 'm2', user_id: 'me', content: 'second', channel_id: 'ch1' })
    let finish: () => void = () => undefined
    vi.spyOn(useChatStore(), 'editMessage').mockImplementation(() => new Promise<null>(r => { finish = () => r(null) }))
    a.startEdit(msg)
    const saving = a.saveEdit(msg)
    a.startEdit(other)
    a.editText.value = 'typed in the new editor'
    finish()
    await saving
    expect(a.editingId.value).toBe('m2')
    expect(a.editText.value).toBe('typed in the new editor')
  })

  it('guards empty and concurrent edits, falls back to message channel and handles non-Error failures', async () => {
    const a = setup()
    useChatStore().activeChannel = null
    const edit = vi.spyOn(useChatStore(), 'editMessage').mockRejectedValue('failed')
    a.startEdit(msg)
    a.editText.value = ' '
    const del = vi.spyOn(useChatStore(), 'deleteMessage').mockResolvedValue(undefined as never)
    const emptied = a.saveEdit(msg)
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    await a.saveEdit(msg) // a second Enter while the prompt is open does not stack another
    pendingConfirm.value?.resolve(false)
    await emptied
    expect(edit).not.toHaveBeenCalled()
    expect(del).not.toHaveBeenCalled()
    expect(a.editingId.value).toBe('m1')
    a.editText.value = 'hello'
    a.isSavingEdit.value = true
    await a.saveEdit(msg)
    expect(edit).not.toHaveBeenCalled()
    a.isSavingEdit.value = false
    await a.saveEdit(msg)
    expect(edit).toHaveBeenCalledWith('ch1', 'm1', 'hello')
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
    useAuthStore().user = null
    expect(a.isOwn(msg)).toBe(false)
  })

  it('does not start editing a poll', () => {
    const a = setup()
    a.startEdit(messageFixture({ id: 'p1', user_id: 'me', content: 'Lunch?', poll: { multiple: false, anonymous: false, closed: false, closes_at: '2099-01-01T00:00:00Z', total_voters: 0, options: [] } }))
    expect(a.editingId.value).toBeNull()
  })

  it('restores the row focus after editing except when another control already has focus', async () => {
    const root = document.createElement('div')
    root.innerHTML = '<div tabindex="0" data-msg-id="m1"></div><button>other</button>'
    document.body.append(root)
    const a = setup({ container: root })
    a.startEdit(msg)
    await nextTick()
    a.cancelEdit()
    await nextTick()
    expect(document.activeElement).toBe(root.firstElementChild)
    a.startEdit(msg)
    await nextTick()
    const button = required(root.querySelector('button'))
    button.focus()
    a.cancelEdit()
    await nextTick()
    expect(document.activeElement).toBe(button)
  })

  it('cancels deletion and covers attachment/empty excerpts and server error fallback', async () => {
    const a = setup()
    const del = vi.spyOn(useChatStore(), 'deleteMessage').mockRejectedValueOnce(new Error('offline')).mockRejectedValueOnce('failed')
    const attachment = messageFixture({ content: '', attachments: [{ id: 'attachment', url: '/api/media/attachment', original_filename: 'test.png', mime_type: 'image/png', size_bytes: 1, is_deleted: false }] })
    const cancelled = a.deleteMessage(attachment)
    expect(pendingConfirm.value?.excerpt).toBe('Attachment')
    required(pendingConfirm.value).resolve(false)
    await cancelled
    expect(del).not.toHaveBeenCalled()
    for (const message of [attachment, messageFixture({ content: '', attachments: [] })]) {
      const request = a.deleteMessage(message)
      required(pendingConfirm.value).resolve(true)
      await request
    }
    expect(useToastStore().toasts.some(toast => toast.text === 'offline')).toBe(true)
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
  })

  it('closes the picker despite a reaction failure', async () => {
    const a = setup()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(useChatStore(), 'toggleReaction').mockRejectedValue('failed')
    a.togglePicker('m1')
    await a.toggleReaction('m1', 'x')
    expect(a.pickerId.value).toBeNull()
    expect(warn).toHaveBeenCalledOnce()
    warn.mockRestore()
  })

  it('edits through the store and leaves edit mode', async () => {
    const a = setup()
    const edit = vi.spyOn(useChatStore(), 'editMessage').mockResolvedValue(null)
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
    expect(required(useToastStore().toasts.at(-1)).text).toBe('nope')
  })

  it('deletes only after confirmation, with the given title', async () => {
    const a = setup({ deleteTitle: 'thread.deleteTitle' })
    const del = vi.spyOn(useChatStore(), 'deleteMessage').mockResolvedValue(undefined)
    const p = a.deleteMessage(msg)
    expect(required(pendingConfirm.value).title).toBe('Delete reply?')
    required(pendingConfirm.value).resolve(true)
    await p
    expect(del).toHaveBeenCalledWith('ch1', 'm1')
  })

  it('knows own messages and who may delete', () => {
    const a = setup()
    expect(a.isOwn(msg)).toBe(true)
    expect(a.isOwn({ ...msg, user_id: 'x' })).toBe(false)
    expect(a.canDelete({ ...msg, user_id: 'x' })).toBe(false)
    useAuthStore().user = userFixture({ id: 'me', username: 'me', role: 'admin' })
    expect(a.canDelete({ ...msg, user_id: 'x' })).toBe(true)
  })

  it('reacting closes the picker', async () => {
    const a = setup()
    const toggle = vi.spyOn(useChatStore(), 'toggleReaction').mockResolvedValue([])
    a.togglePicker('m1')
    await a.toggleReaction('m1', '🔥')
    expect(toggle).toHaveBeenCalledWith('m1', '🔥')
    expect(a.pickerId.value).toBe(null)
  })
})

describe('useMessageActions reaction picker', () => {
  it('closes a picker without a mounted list container on an outside press', async () => {
    const actions = setup()
    actions.togglePicker('m1')
    await nextTick()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(actions.pickerId.value).toBeNull()
  })
  it('closes on a press outside the picker anchors and on Escape', async () => {
    const container = document.createElement('div')
    container.innerHTML = '<div class="reaction-picker-anchor"><button id="in"></button></div><p id="out"></p>'
    document.body.appendChild(container)
    const a = setup({ container: ref(container) })

    a.togglePicker('m1')
    await nextTick()
    required(container.querySelector('#in')).dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(a.pickerId.value).toBe('m1')
    required(container.querySelector('#out')).dispatchEvent(new Event('pointerdown', { bubbles: true }))
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

describe('useMessageActions editing', () => {
  const ID = '11111111-2222-4333-8444-555555555555'

  it('shows custom emoji as :name: in the edit box instead of the stored token', () => {
    const a = setup()
    a.startEdit(messageFixture({ id: 'm1', user_id: 'me', content: `hello <:blob:${ID}> world` }))
    expect(a.editText.value).toBe('hello :blob: world')
  })
})

describe('formatTime', () => {
  it('returns hours and minutes, empty for no date', () => {
    expect(formatTime('')).toBe('')
    expect(formatTime('2026-01-01T10:05:00Z')).toMatch(/\d{2}:\d{2}/)
  })
})
