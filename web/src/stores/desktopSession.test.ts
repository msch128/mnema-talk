import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { userFixture } from '../test-fixtures.fixture'

const request = vi.hoisted(() => vi.fn())
vi.mock('../lib/api', async importOriginal => ({
  ...await importOriginal<typeof import('../lib/api')>(),
  api: request
}))
import { useAuthStore } from './auth'

beforeEach(() => { setActivePinia(createPinia()); request.mockReset() })

it('a server switch retires a pending login before its old instance can install a user', async () => {
  let resolve!: (value: unknown) => void
  request.mockImplementation(() => new Promise(done => { resolve = done }))
  const auth = useAuthStore()
  const login = auth.login('synthetic-user', 'synthetic-password')
  await Promise.resolve()
  auth.resetLocalSession()
  resolve({ user: userFixture() })
  await login
  expect(auth.user).toBeNull()
})

it('retires an in-flight account lookup when changing instances', async () => {
  let resolve!: (value: unknown) => void
  request.mockImplementation(() => new Promise(done => { resolve = done }))
  const auth = useAuthStore()
  const lookup = auth.checkAuth()
  await Promise.resolve()
  auth.resetLocalSession()
  resolve(userFixture())
  expect(await lookup).toBeNull()
  expect(auth.user).toBeNull()
})
