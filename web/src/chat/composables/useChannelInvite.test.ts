import type { ApiOptions } from '../../shared/lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { expiryChoices, useChannelInvite } from './useChannelInvite'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { setLocale } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const limits = { max_expires_in_hours: 168, max_uses: 25, max_active: 10, max_per_hour: 10 }
const inviteOf = (id: string, code: string) => ({
  id, code, max_uses: 10, uses_count: 0, expires_at: '2026-12-01T00:00:00Z', created_at: '2026-11-01T00:00:00Z', weak: false,
  channel_id: 'chan', created_by_id: 'me', created_by_name: 'maya'
})

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('expiryChoices', () => {
  it('offers the usual choices within the limit, or the limit itself', () => {
    expect(expiryChoices(168)).toEqual([1, 24, 168])
    expect(expiryChoices(48)).toEqual([1, 24])
    expect(expiryChoices(1)).toEqual([1])
    expect(expiryChoices(0)).toEqual([0])
  })
})

describe('useChannelInvite', () => {
  it('makes a link for the channel, a day long, from the public address', async () => {
    useServerFeaturesStore().publicUrl = 'https://chat.example.org'
    apiMock.mockResolvedValueOnce({ invites: [], limits }).mockResolvedValueOnce(inviteOf('i1', 'abc123xyz789'))
    const invite = useChannelInvite(() => 'chan')
    await invite.start()
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/invites', { method: 'POST', json: { channel_id: 'chan', expires_in_hours: 24 }, decode: expect.any(Function) })
    expect(invite.link.value).toBe('https://chat.example.org/invite/abc123xyz789')
    expect(invite.busy.value).toBe(false)
    expect(invite.own.value.map(i => i.id)).toEqual(['i1'])
    expect(invite.choices.value).toEqual([1, 24, 168])
  })

  it('starts with the longest choice that fits when the limit is below a day', async () => {
    apiMock.mockResolvedValueOnce({ invites: [], limits: { ...limits, max_expires_in_hours: 1 } }).mockResolvedValueOnce(inviteOf('i1', 'abc123xyz789'))
    const invite = useChannelInvite(() => 'chan')
    await invite.start()
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/invites', expect.objectContaining({ json: { channel_id: 'chan', expires_in_hours: 1 } }))
  })

  it('replaces the link it made before when another expiry is chosen', async () => {
    apiMock.mockResolvedValueOnce({ invites: [inviteOf('old', 'oldcode12345')], limits }).mockResolvedValueOnce(inviteOf('i1', 'abc123xyz789'))
    const invite = useChannelInvite(() => 'chan')
    await invite.start()
    apiMock.mockResolvedValueOnce(inviteOf('i2', 'def456uvw012')).mockResolvedValueOnce(undefined)
    await invite.make(168)
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/invites/i1', { method: 'DELETE' })
    expect(invite.hours.value).toBe(168)
    expect(invite.own.value.map(i => i.id)).toEqual(['i2', 'old'])
  })

  it('says why no link was made and keeps the old one', async () => {
    apiMock.mockResolvedValueOnce({ invites: [], limits }).mockRejectedValueOnce(new Error('You already have 10 active invites. Revoke one first.'))
    const invite = useChannelInvite(() => 'chan')
    await invite.start()
    expect(invite.error.value).toBe('You already have 10 active invites. Revoke one first.')
    expect(invite.link.value).toBe('')
    expect(invite.busy.value).toBe(false)
  })

  it('reports a failed first load and makes nothing', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const invite = useChannelInvite(() => 'chan')
    await invite.start()
    expect(invite.error.value).toBe('offline')
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(invite.busy.value).toBe(false)
  })

  it('revokes an invite of its own, and the link goes with it', async () => {
    apiMock.mockResolvedValueOnce({ invites: [], limits }).mockResolvedValueOnce(inviteOf('i1', 'abc123xyz789'))
    const invite = useChannelInvite(() => 'chan')
    await invite.start()
    apiMock.mockResolvedValueOnce(undefined)
    expect(await invite.revoke(inviteOf('i1', 'abc123xyz789'))).toBe(true)
    expect(invite.own.value).toEqual([])
    expect(invite.invite.value).toBeNull()
    apiMock.mockRejectedValueOnce(new Error('nope'))
    expect(await invite.revoke(inviteOf('i9', 'zzzzzzzzzzzz'))).toBe(false)
  })
})
