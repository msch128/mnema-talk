import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChannelInviteHost from './ChannelInviteHost.vue'
import { channelInviteFor, closeChannelInvite, openChannelInvite } from '../lib/channelInvite'
import { useChatStore } from '../../stores/chat'
import { channelFixture } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'

const limits = { max_expires_in_hours: 168, max_uses: 25, max_active: 10, max_per_hour: 10 }
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: vi.fn((path: string) => Promise.resolve(path === '/api/me/invites'
    ? { invites: [], limits }
    : { id: 'i1', code: 'abc123xyz789', max_uses: 10, uses_count: 0, expires_at: null, created_at: '2026-11-01T00:00:00Z', weak: false, channel_id: 'chan-1', created_by_id: 'me', created_by_name: 'maya' }))
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  closeChannelInvite()
  useChatStore().uncategorized = [channelFixture({ id: 'chan-1', name: 'general' })]
})
afterEach(() => { document.body.innerHTML = '' })

describe('ChannelInviteHost', () => {
  it('shows the dialog for the channel asked for, and nothing before', async () => {
    const w = mount(ChannelInviteHost, { attachTo: document.body })
    expect(document.querySelector('[data-testid="channel-invite"]')).toBeNull()
    openChannelInvite('chan-1')
    await flushPromises()
    expect(document.body.textContent).toContain('Invite people to general')
    w.unmount()
  })

  it('closes by itself when the channel is gone', async () => {
    const w = mount(ChannelInviteHost, { attachTo: document.body })
    openChannelInvite('chan-1')
    await flushPromises()
    useChatStore().uncategorized = []
    await flushPromises()
    expect(channelInviteFor.value).toBeNull()
    expect(document.querySelector('[data-testid="channel-invite"]')).toBeNull()
    w.unmount()
  })
})
