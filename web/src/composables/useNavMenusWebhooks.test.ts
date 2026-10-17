import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { buildChannelItems } from './useNavMenus'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { PERMISSION_BITS } from '../lib/permissions'
import { channelFixture, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

vi.mock('../lib/api', async original => ({ ...(await original<typeof import('../lib/api')>()), api: vi.fn() }))

const channel = channelFixture({ id: 'c1', type: 'text', name: 'general' })
const ids = (items: ReturnType<typeof buildChannelItems>) => items.map(i => i.id)

function asMember(bits: number, owner = false) {
  useAuthStore().user = userFixture({ id: 'me', role: owner ? 'admin' : 'user' })
  usePermissionsStore().info = {
    owner, permissions: bits, role_ids: [], top_position: 1, catalog: [],
    channels: [{ channel_id: 'c1', permissions: bits | PERMISSION_BITS.view_channel }]
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('the Webhooks entry of the channel menu', () => {
  it('is for the Owner and Administrators only', () => {
    // Not even Manage webhooks, Manage channels or Manage roles opens webhooks.
    asMember(PERMISSION_BITS.manage_webhooks | PERMISSION_BITS.manage_roles)
    expect(ids(buildChannelItems(channel))).not.toContain('webhooks')
    asMember(PERMISSION_BITS.manage_channels | PERMISSION_BITS.manage_webhooks)
    expect(ids(buildChannelItems(channel))).not.toContain('webhooks')
    const onWebhooks = vi.fn()
    asMember(PERMISSION_BITS.administrator)
    const items = buildChannelItems(channel, { onWebhooks })
    expect(ids(items)).toContain('webhooks')
    items.find(i => i.id === 'webhooks')?.action?.()
    expect(onWebhooks).toHaveBeenCalledWith(channel)
    asMember(0, true)
    expect(ids(buildChannelItems(channel))).toContain('webhooks')
  })

  it('sits with the channel management entries for channel managers, and tolerates a missing handler', () => {
    asMember(PERMISSION_BITS.administrator | PERMISSION_BITS.manage_channels)
    const items = buildChannelItems(channel)
    expect(ids(items)).toContain('webhooks')
    expect(ids(items).indexOf('webhooks')).toBeLessThan(ids(items).indexOf('permissions'))
    expect(() => items.find(i => i.id === 'webhooks')?.action?.()).not.toThrow()
    asMember(PERMISSION_BITS.administrator)
    expect(ids(buildChannelItems(channel))).not.toContain('permissions')
  })
})
