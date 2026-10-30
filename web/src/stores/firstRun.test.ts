import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useFirstRunStore } from './firstRun'
import { useAuthStore } from './auth'
import { userFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const steps = (done: string[] = []) => ['name', 'invite', 'channels', 'voice', 'status', 'roles_rules'].map(id => ({ id, done: done.includes(id), optional: id === 'roles_rules' }))
const checklist = (over: Record<string, unknown> = {}) => ({ steps: steps(), hidden: false, complete: false, ...over })

function signIn(role: 'admin' | 'user') {
  useAuthStore().user = userFixture({ role })
}

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
})

describe('the Owner\'s first-run checklist store', () => {
  it('reads the checklist for the Owner only', async () => {
    signIn('user')
    const member = useFirstRunStore()
    await member.ensureLoaded()
    expect(apiMock).not.toHaveBeenCalled()
    expect(member.visible).toBe(false)

    setActivePinia(createPinia())
    signIn('admin')
    apiMock.mockResolvedValue(checklist({ steps: steps(['name']) }))
    const owner = useFirstRunStore()
    await owner.ensureLoaded()
    await owner.ensureLoaded()
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/first-run', expect.anything())
    expect(owner.visible).toBe(true)
    expect(owner.done).toBe(1)
  })

  it('stays out of sight when the read fails or the checklist is closed', async () => {
    signIn('admin')
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const store = useFirstRunStore()
    await store.reload()
    expect(store.visible).toBe(false)
    apiMock.mockResolvedValueOnce(checklist({ hidden: true }))
    await store.reload()
    expect(store.visible).toBe(false)
  })

  it('closes and reopens at once, and takes the close back when it cannot be stored', async () => {
    signIn('admin')
    apiMock.mockResolvedValueOnce(checklist())
    const store = useFirstRunStore()
    await store.reload()
    apiMock.mockResolvedValueOnce(checklist({ hidden: true }))
    const closing = store.setHidden(true)
    expect(store.visible).toBe(false)
    await closing
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/first-run', expect.objectContaining({ method: 'PUT', json: { hidden: true } }))

    apiMock.mockRejectedValueOnce(new Error('offline'))
    await store.setHidden(false)
    expect(store.state?.hidden).toBe(true)
  })

  it('reports a visit once and not at all for a step that is done', async () => {
    signIn('admin')
    apiMock.mockResolvedValueOnce(checklist())
    const store = useFirstRunStore()
    await store.reload()
    apiMock.mockResolvedValueOnce(checklist({ steps: steps(['channels']) }))
    await store.visit('channels')
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/first-run', expect.objectContaining({ method: 'PUT', json: { visited: ['channels'] } }))
    const calls = apiMock.mock.calls.length
    await store.visit('channels')
    expect(apiMock.mock.calls.length).toBe(calls)
    expect(store.done).toBe(1)
  })

  it('forgets everything on sign-out', async () => {
    signIn('admin')
    apiMock.mockResolvedValue(checklist())
    const store = useFirstRunStore()
    await store.reload()
    store.reset()
    expect(store.state).toBeNull()
    await store.ensureLoaded()
    expect(apiMock).toHaveBeenCalledTimes(2)
  })
})
