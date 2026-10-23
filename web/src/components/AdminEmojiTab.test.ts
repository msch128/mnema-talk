import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminEmojiTab from './AdminEmojiTab.vue'
import EmojiGlyph from './EmojiGlyph.vue'
import { setLocale } from '../i18n'
import { useEmojiStore } from '../stores/emoji'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (...args: unknown[]) => confirmMock(...args) }))

const ID = '00000000-0000-4000-8000-000000000501'
const entry = { id: ID, name: 'blob', animated: false, url: '/api/emoji/x/image?v=1', created_at: '2026-01-01T00:00:00Z' }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  apiMock.mockImplementation(async (path, options) => {
    if (path === '/api/emoji') return { emoji: [entry], limit: 5 }
    return options?.method === 'DELETE' ? undefined : entry
  })
})

describe('AdminEmojiTab', () => {
  it('lists the emoji with the count of the limit', async () => {
    const w = mount(AdminEmojiTab)
    await flushPromises()
    expect(w.findAll('[data-testid="emoji-row"]')).toHaveLength(1)
    expect(w.get('[data-testid="emoji-count"]').text()).toBe('1 of 5 used')
    expect(w.text()).toContain(':blob:')
  })

  it('uploads the chosen file under a name proposed from its file name', async () => {
    const w = mount(AdminEmojiTab)
    await flushPromises()
    const input = w.get<HTMLInputElement>('[data-testid="emoji-file"]')
    const file = new File(['x'], 'Party Blob.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    expect(w.get('[data-testid="emoji-file-name"]').text()).toBe('No file yet')
    await input.trigger('change')
    expect(w.get<HTMLInputElement>('[data-testid="emoji-name"]').element.value).toBe('party_blob')
    expect(w.get('[data-testid="emoji-file-name"]').text()).toBe('Party Blob.png')
    await w.get('form').trigger('submit')
    await flushPromises()
    const post = apiMock.mock.calls.find(([p, o]) => p === '/api/admin/emoji' && o?.method === 'POST')
    expect((post?.[1]?.form as FormData).get('name')).toBe('party_blob')
    expect(w.get<HTMLInputElement>('[data-testid="emoji-name"]').element.value).toBe('')
  })

  it('does not upload without a valid name', async () => {
    const w = mount(AdminEmojiTab)
    await flushPromises()
    await w.get('[data-testid="emoji-name"]').setValue('x')
    expect(w.get<HTMLButtonElement>('[data-testid="emoji-upload"]').element.disabled).toBe(true)
  })

  it('renames an emoji', async () => {
    const w = mount(AdminEmojiTab)
    await flushPromises()
    await w.get('button[aria-label="Rename"]').trigger('click')
    await w.get('[data-testid="emoji-row"] input').setValue('Blobby')
    await w.get('[data-testid="emoji-row"] form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/emoji/${ID}`, expect.objectContaining({ method: 'PATCH', json: { name: 'blobby' } }))
  })

  it('deletes an emoji only after confirmation', async () => {
    const w = mount(AdminEmojiTab)
    await flushPromises()
    confirmMock.mockResolvedValueOnce(false)
    await w.get('button[aria-label="Delete"]').trigger('click')
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalledWith(`/api/admin/emoji/${ID}`, expect.anything())
    confirmMock.mockResolvedValueOnce(true)
    await w.get('button[aria-label="Delete"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/emoji/${ID}`, { method: 'DELETE' })
  })

  it('stops offering uploads when the limit is reached', async () => {
    apiMock.mockImplementation(async () => ({ emoji: [entry], limit: 1 }))
    const w = mount(AdminEmojiTab)
    await flushPromises()
    expect(w.get<HTMLInputElement>('[data-testid="emoji-file"]').element.disabled).toBe(true)
    expect(w.text()).toContain('The limit is reached')
  })
})

describe('EmojiGlyph', () => {
  it('shows a token as its image, a deleted one as :name:, and plain emoji as typed', async () => {
    const store = useEmojiStore()
    store.items = [{ id: ID, name: 'blob', animated: false, url: '/api/emoji/x/image?v=1' }]
    const token = `<:blob:${ID}>`
    expect(mount(EmojiGlyph, { props: { value: token } }).get('img').attributes('alt')).toBe(':blob:')
    expect(mount(EmojiGlyph, { props: { value: '<:gone:11111111-2222-4333-8444-555555555555>' } }).text()).toBe(':gone:')
    expect(mount(EmojiGlyph, { props: { value: '👍' } }).text()).toBe('👍')
  })
})
