import type { ApiOptions } from '../../shared/lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { canInviteInto, channelInviteFor, closeChannelInvite, inviteMenuItems, openChannelInvite } from './channelInvite'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { usePermissionsStore } from '../../stores/permissions'
import { useAuthStore } from '../../stores/auth'
import { fixtureId, userFixture } from '../../test-fixtures.fixture'
import { DEFAULT_EVERYONE, PERMISSION_BITS } from '../../shared/lib/permissions'
import { setLocale } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

beforeEach(() => {
  apiMock.mockReset()
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ role: 'user' })
  closeChannelInvite()
})

function serverOffers(on: boolean) {
  useServerFeaturesStore().list = on ? ['invites.member'] : []
}

describe('the invite entry of a channel menu', () => {
  it('is offered when the server has member invites and the member may invite in the channel', () => {
    serverOffers(true)
    // Before the answer a member is assumed to hold what @everyone holds, which includes Create invites on a fresh install.
    expect(canInviteInto('c1')).toBe(true)
    const [item] = inviteMenuItems('c1')
    expect(item?.id).toBe('invite-people')
    expect(item?.label).toBe('Invite people')
  })

  it('is hidden on a server without the feature', () => {
    serverOffers(false)
    expect(inviteMenuItems('c1')).toEqual([])
  })

  it('is hidden where the member lacks the permission', async () => {
    serverOffers(true)
    const channel = fixtureId(2)
    apiMock.mockResolvedValue({
      owner: false, permissions: DEFAULT_EVERYONE, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [],
      channels: [{ channel_id: channel, permissions: PERMISSION_BITS.view_channel }]
    })
    await usePermissionsStore().load()
    expect(inviteMenuItems(channel)).toEqual([])
    expect(canInviteInto(channel)).toBe(false)
  })

  it('opens and closes the dialog for one channel', () => {
    serverOffers(true)
    const [item] = inviteMenuItems('c1')
    item?.action?.()
    expect(channelInviteFor.value).toBe('c1')
    openChannelInvite('c2')
    expect(channelInviteFor.value).toBe('c2')
    closeChannelInvite()
    expect(channelInviteFor.value).toBeNull()
  })
})
