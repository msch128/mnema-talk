import type { ApiOptions } from '../../shared/lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChannelInviteDialog from './ChannelInviteDialog.vue'
import { useToastStore } from '../../stores/toast'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { channelFixture } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))
const copyMock = vi.fn<(text: string) => Promise<void>>()

const limits = { max_expires_in_hours: 168, max_uses: 25, max_active: 10, max_per_hour: 10 }
const inviteOf = (id: string, code: string, over: Record<string, unknown> = {}) => ({
  id, code, max_uses: 10, uses_count: 2, expires_at: '2099-01-01T00:00:00Z', created_at: '2026-11-01T00:00:00Z', weak: false,
  channel_id: 'chan', created_by_id: 'me', created_by_name: 'maya', ...over
})
const channel = channelFixture({ id: 'chan', name: 'general' })
let wrapper: VueWrapper

async function open(own: unknown[] = []) {
  apiMock.mockResolvedValueOnce({ invites: own, limits }).mockResolvedValueOnce(inviteOf('fresh', 'abc123xyz789', { uses_count: 0 }))
  wrapper = mount(ChannelInviteDialog, { props: { channel }, attachTo: document.body })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  copyMock.mockReset()
  useServerFeaturesStore().publicUrl = 'https://chat.example.org'
  vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: copyMock } })
})
afterEach(() => { wrapper?.unmount(); vi.unstubAllGlobals() })

describe('ChannelInviteDialog', () => {
  it('shows a ready link into the channel and copies it', async () => {
    const w = await open()
    expect(document.body.textContent).toContain('Invite people to general')
    expect(document.querySelector<HTMLInputElement>('[data-testid="channel-invite-link"]')?.value).toBe('https://chat.example.org/invite/abc123xyz789')
    copyMock.mockResolvedValue()
    document.querySelector<HTMLButtonElement>('[data-testid="channel-invite-copy"]')?.click()
    await flushPromises()
    expect(copyMock).toHaveBeenCalledWith('https://chat.example.org/invite/abc123xyz789')
    expect(useToastStore().toasts[0]?.text).toBe('Link copied')
    w.unmount()
  })

  it('says when the link could not be copied', async () => {
    await open()
    copyMock.mockRejectedValue(new Error('denied'))
    document.querySelector<HTMLButtonElement>('[data-testid="channel-invite-copy"]')?.click()
    await flushPromises()
    expect(useToastStore().toasts[0]?.type).toBe('error')
  })

  it('makes a new link when another expiry is picked', async () => {
    await open()
    apiMock.mockResolvedValueOnce(inviteOf('week', 'week12345678', { uses_count: 0 })).mockResolvedValueOnce(undefined)
    document.querySelector<HTMLButtonElement>('[data-hours="168"]')?.click()
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/me/invites', expect.objectContaining({ method: 'POST', json: { channel_id: 'chan', expires_in_hours: 168 } }))
    expect(document.querySelector<HTMLInputElement>('[data-testid="channel-invite-link"]')?.value).toContain('week12345678')
    expect(document.querySelector('[data-hours="168"]')?.getAttribute('aria-pressed')).toBe('true')
  })

  it('lists the member’s own invites and revokes one', async () => {
    await open([inviteOf('mine', 'mine12345678'), inviteOf('gone', 'gone12345678', { expires_at: '2000-01-01T00:00:00Z' })])
    expect(document.querySelector('[data-testid="own-invites"]')?.textContent).toContain('mine12345678')
    expect(document.querySelector('[data-testid="own-invites"]')?.textContent).toContain('Expired')
    apiMock.mockResolvedValueOnce(undefined)
    document.querySelector<HTMLButtonElement>('[data-testid="own-invite-revoke"]')?.click()
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/invites/fresh', { method: 'DELETE' })
    expect(useToastStore().toasts[0]?.text).toBe('Invite revoked')
  })

  it('reports a refused revoke', async () => {
    await open()
    apiMock.mockRejectedValueOnce(new Error('no'))
    document.querySelector<HTMLButtonElement>('[data-testid="own-invite-revoke"]')?.click()
    await flushPromises()
    expect(useToastStore().toasts[0]?.type).toBe('error')
  })

  it('shows the server’s reason when no link can be made, with the invites to revoke', async () => {
    apiMock.mockResolvedValueOnce({ invites: [inviteOf('mine', 'mine12345678')], limits }).mockRejectedValueOnce(new Error('You already have 10 active invites. Revoke one first.'))
    wrapper = mount(ChannelInviteDialog, { props: { channel }, attachTo: document.body })
    await flushPromises()
    expect(document.querySelector('[data-testid="channel-invite-error"]')?.textContent).toContain('Revoke one first')
    expect(document.querySelector<HTMLButtonElement>('[data-testid="channel-invite-copy"]')?.disabled).toBe(true)
    expect(document.querySelector('[data-testid="own-invites"]')?.textContent).toContain('mine12345678')
  })

  it('closes with Done', async () => {
    const w = await open()
    ;[...document.querySelectorAll('button')].find(b => b.textContent?.trim() === 'Done')?.click()
    expect(w.emitted('close')).toHaveLength(1)
  })
})
