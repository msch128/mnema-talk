import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { api } from './api'
import { moderationName, moderationOffer, liftTimeout, openTimeout, openBan } from './moderationActions'
import { pendingModeration } from './moderation'
import { PERMISSION_BITS } from './permissions'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { userFixture } from '../test-fixtures.fixture'

vi.mock('./api', async original => ({ ...(await original<typeof import('./api')>()), api: vi.fn() }))

const info = (bits: number) => ({ owner: false, permissions: bits, role_ids: [], top_position: 5, max_upload_mb: 50, catalog: [], channels: [] })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.mocked(api).mockReset()
  pendingModeration.value = null
  useAuthStore().user = userFixture({ id: 'me', role: 'user' })
})

describe('moderation actions', () => {
  it('names a member by display name, user name or id', () => {
    expect(moderationName({ id: 'x', display_name: 'D', username: 'u' })).toBe('D')
    expect(moderationName({ id: 'x', username: 'u' })).toBe('u')
    expect(moderationName({ id: 'x' })).toBe('x')
  })

  it('offers what the permissions and rank allow, never for oneself or the Owner', () => {
    const permissions = usePermissionsStore()
    const chat = useChatStore()
    permissions.info = info(0)
    expect(moderationOffer({ id: 'u2' })).toEqual({ timeout: false, removeTimeout: false, ban: false })
    permissions.info = info(PERMISSION_BITS.timeout_members | PERMISSION_BITS.ban_members)
    expect(moderationOffer({ id: 'u2' })).toEqual({ timeout: true, removeTimeout: false, ban: true })
    expect(moderationOffer({ id: 'me' })).toEqual({ timeout: false, removeTimeout: false, ban: false })
    expect(moderationOffer({ id: 'u2', role: 'admin' })).toEqual({ timeout: false, removeTimeout: false, ban: false })
    chat.members = [{ ...userFixture({ id: 'u2' }), timeout_until: new Date(Date.now() + 60_000).toISOString() } as never]
    expect(moderationOffer({ id: 'u2' }).removeTimeout).toBe(true)
    chat.members = [{ ...userFixture({ id: 'u2' }), timeout_until: new Date(Date.now() - 60_000).toISOString() } as never]
    expect(moderationOffer({ id: 'u2' }).removeTimeout).toBe(false)
    chat.members = []
  })

  it('opens the dialogs for a member', () => {
    void openTimeout({ id: 'u2', display_name: 'Two' })
    expect(pendingModeration.value).toMatchObject({ kind: 'timeout', target: { id: 'u2', name: 'Two' } })
    void openBan({ id: 'u3', username: 'three' })
    expect(pendingModeration.value).toMatchObject({ kind: 'ban', target: { id: 'u3', name: 'three' } })
  })

  it('lifts a timeout and says so, or reports the failure', async () => {
    vi.mocked(api).mockResolvedValueOnce(undefined)
    expect(await liftTimeout({ id: 'u2', display_name: 'Two' })).toBe(true)
    expect(api).toHaveBeenCalledWith('/api/admin/users/u2/timeout', { method: 'DELETE' })
    expect(useToastStore().toasts.map(t => t.text)).toContain('Timeout for Two ended.')
    vi.mocked(api).mockRejectedValueOnce(new Error('no'))
    expect(await liftTimeout({ id: 'u2' })).toBe(false)
    expect(useToastStore().toasts.map(t => t.text)).toContain('no')
  })
})
