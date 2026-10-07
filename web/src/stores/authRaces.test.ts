import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { api, ApiError } from '../lib/api'
import { locale, setLocale } from '../i18n'
import { userFixture, fixtureId, requireValue } from '../test-fixtures.fixture'
import { decodeUserEnvelope, type User } from '../types/domain'

interface PendingRequest { path: string; resolve: (response: Response) => void; reject: (error: unknown) => void }
let requests: PendingRequest[]
// Response-time cookie effects model the browser's HttpOnly Set-Cookie behavior;
// auth.ts cannot inspect this value. No secret is represented or persisted.
let serverCookie: { user: User; version: number } | null
let cookieVersion: number
const alice = userFixture({ id: fixtureId(101), username: 'alice', display_name: 'Alice', locale: 'de' })
const bob = userFixture({ id: fixtureId(102), username: 'bob', display_name: 'Bob', locale: 'en' })
function request(path: string, index = 0) { return requireValue(requests.filter(item => item.path === path)[index]) }
async function started(path: string, index = 0) {
  await vi.waitFor(() => expect(requests.filter(item => item.path === path).length).toBeGreaterThan(index))
  return request(path, index)
}
async function answer(path: string, body: unknown = null, status = 200, index = 0) {
  const pending = await started(path, index)
  if ((path === '/api/auth/login' || path === '/api/auth/register') && status === 200) {
    serverCookie = { user: decodeUserEnvelope(body).user, version: ++cookieVersion }
  }
  if (path === '/api/auth/logout' && status === 204) serverCookie = null
  if (path === '/api/auth/password' && status === 204 && serverCookie) serverCookie = { user: serverCookie.user, version: ++cookieVersion }
  pending.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
}
function beginLogin(auth: ReturnType<typeof useAuthStore>, account: User) {
  return auth.login(account.username, 'synthetic-test-password')
}
function beginRegister(auth: ReturnType<typeof useAuthStore>, account: User) {
  return auth.register(account.username, account.display_name, 'synthetic-test-password', 'synthetic-invite')
}
async function loginAs(auth: ReturnType<typeof useAuthStore>, account: User) {
  const index = requests.filter(item => item.path === '/api/auth/login').length
  const pending = beginLogin(auth, account)
  await answer('/api/auth/login', { user: account }, 200, index)
  await pending
}
function signedIn() {
  const auth = useAuthStore()
  auth.user = alice
  serverCookie = { user: alice, version: ++cookieVersion }
  return auth
}
beforeEach(() => {
  localStorage.clear(); setLocale('de'); setActivePinia(createPinia())
  requests = []; serverCookie = null; cookieVersion = 0
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(url => new Promise<Response>((resolve, reject) => requests.push({ path: String(url), resolve, reject }))))
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('session restore races', () => {
  it.each([204, 503])('never restores pending /me after logout returns %s', async status => {
    const auth = signedIn(), checking = auth.checkAuth()
    await started('/api/auth/me')
    const loggingOut = auth.logout()
    expect(auth.user).toBeNull()
    const outcome = status === 204 ? loggingOut : expect(loggingOut).rejects.toBeInstanceOf(ApiError)
    await answer('/api/auth/logout', null, status); await outcome
    await answer('/api/auth/me', { ...alice, locale: 'en' })
    expect(await checking).toBeNull(); expect(auth.user).toBeNull(); expect(locale.value).toBe('de')
  })
  it.each([200, 401, 503])('ignores older /me status %s after new login', async status => {
    const auth = signedIn(), checking = auth.checkAuth()
    await started('/api/auth/me'); await loginAs(auth, bob)
    await answer('/api/auth/me', status === 200 ? alice : { error: { code: 'UNAUTHORIZED' } }, status)
    expect(await checking).toBeNull(); expect(auth.user).toEqual(bob); expect(locale.value).toBe('en')
  })
  it('refreshes current account and still clears current revoked session', async () => {
    const auth = signedIn(), checking = auth.checkAuth()
    await answer('/api/auth/me', { ...alice, display_name: 'Refreshed Alice' }); expect(await checking).toBe(true)
    expect(auth.user?.display_name).toBe('Refreshed Alice')
    const revoked = auth.checkAuth()
    await answer('/api/auth/me', { error: { code: 'UNAUTHORIZED' } }, 401, 1)
    expect(await revoked).toBe(false); expect(auth.user).toBeNull()
  })
  it('does not overwrite account changed while /me pending', async () => {
    const auth = signedIn(), checking = auth.checkAuth()
    await started('/api/auth/me'); auth.user = bob
    await answer('/api/auth/me', alice); expect(await checking).toBeNull(); expect(auth.user).toEqual(bob)
  })
  it('invalidates pending restore on store disposal', async () => {
    const auth = useAuthStore(), checking = auth.checkAuth()
    await started('/api/auth/me'); auth.$dispose()
    await answer('/api/auth/me', alice); expect(await checking).toBeNull(); expect(auth.user).toBeNull()
  })
  it('waits for cookie-changing login rather than checking the prior cookie', async () => {
    const auth = signedIn(), login = beginLogin(auth, bob)
    await started('/api/auth/login')
    const checking = auth.checkAuth()
    await Promise.resolve(); expect(requests.some(item => item.path === '/api/auth/me')).toBe(false)
    await answer('/api/auth/login', { user: bob }); await login
    expect(await checking).toBeNull(); expect(auth.user).toEqual(bob)
    expect(requests.some(item => item.path === '/api/auth/me')).toBe(false)
  })
})

describe('ordered HttpOnly cookie mutations', () => {
  it.each(['login', 'register'] as const)('finishes pending %s before logout clears its cookie', async operation => {
    const auth = useAuthStore()
    const authentication = operation === 'login' ? beginLogin(auth, alice) : beginRegister(auth, alice)
    await started(`/api/auth/${operation}`)
    const loggingOut = auth.logout()
    expect(auth.user).toBeNull(); expect(requests.some(item => item.path === '/api/auth/logout')).toBe(false)
    await answer(`/api/auth/${operation}`, { user: alice }); await authentication
    expect(auth.user).toBeNull(); expect(serverCookie?.user).toEqual(alice)
    await answer('/api/auth/logout', null, 204); await loggingOut
    expect(serverCookie).toBeNull()
    const fresh = auth.checkAuth()
    await answer('/api/auth/me', { error: { code: 'UNAUTHORIZED' } }, 401)
    expect(await fresh).toBe(false); expect(auth.user).toBeNull()
  })
  it.each(['login', 'register'] as const)('does not revive externally removed account during pending %s', async operation => {
    const auth = signedIn()
    const pending = operation === 'login' ? beginLogin(auth, alice) : beginRegister(auth, alice)
    await started(`/api/auth/${operation}`); auth.user = null
    await answer(`/api/auth/${operation}`, { user: alice }); await pending
    expect(auth.user).toBeNull()
  })
  it('orders login and subsequent registration, keeping newest account and cookie', async () => {
    const auth = useAuthStore(), older = beginLogin(auth, alice), newer = beginRegister(auth, bob)
    await started('/api/auth/login'); expect(requests.some(item => item.path === '/api/auth/register')).toBe(false)
    await answer('/api/auth/login', { user: alice }); await older
    await answer('/api/auth/register', { user: bob }); await newer
    expect(auth.user).toEqual(bob); expect(serverCookie?.user).toEqual(bob); expect(locale.value).toBe('en')
  })
  it.each([204, 503, 401])('finishes old logout %s before newer login sets its cookie', async status => {
    const auth = signedIn(), loggingOut = auth.logout()
    const outcome = status === 204 ? loggingOut : expect(loggingOut).rejects.toBeInstanceOf(ApiError)
    const login = beginLogin(auth, bob)
    await started('/api/auth/logout'); expect(requests.some(item => item.path === '/api/auth/login')).toBe(false)
    await answer('/api/auth/logout', null, status); await outcome
    await answer('/api/auth/login', { user: bob }); await login
    expect(auth.user).toEqual(bob); expect(serverCookie?.user).toEqual(bob)
  })
  it.each(['login', 'register'] as const)('clears misleading old UI when earlier %s sets B cookie and latest login fails', async operation => {
    for (const status of [401, 503]) {
      requests = []
      const auth = signedIn()
      const first = operation === 'login' ? beginLogin(auth, bob) : beginRegister(auth, bob)
      const third = userFixture({ id: fixtureId(103), username: 'carol', locale: 'de' })
      const latest = beginLogin(auth, third), failure = expect(latest).rejects.toBeInstanceOf(ApiError)
      await answer(`/api/auth/${operation}`, { user: bob }); await first
      const index = operation === 'login' ? 1 : 0
      await answer('/api/auth/login', { error: { code: status === 401 ? 'INVALID_CREDENTIALS' : 'UNAVAILABLE' } }, status, index)
      await failure
      expect(serverCookie?.user).toEqual(bob)
      expect(auth.user).toBeNull(); expect(auth.isAuthenticated).toBe(false)
      const fresh = auth.checkAuth(); await answer('/api/auth/me', serverCookie?.user)
      expect(await fresh).toBe(true); expect(auth.user).toEqual(bob)
    }
  })

  it('preserves known current account when newest login fails without cookie identity change', async () => {
    const auth = signedIn()
    const refresh = auth.checkAuth(); await answer('/api/auth/me', alice); await refresh
    const login = beginLogin(auth, bob), failure = expect(login).rejects.toBeInstanceOf(ApiError)
    await answer('/api/auth/login', { error: { code: 'INVALID_CREDENTIALS' } }, 401); await failure
    expect(auth.user).toEqual(alice); expect(serverCookie?.user).toEqual(alice)
  })

  it('current registration 401 invalidates current account and queue still recovers', async () => {
    const auth = signedIn(), registration = beginRegister(auth, bob)
    const failure = expect(registration).rejects.toBeInstanceOf(ApiError)
    await answer('/api/auth/register', { error: { code: 'UNAUTHORIZED' } }, 401); await failure
    expect(auth.user).toBeNull()
    await loginAs(auth, bob); expect(auth.user).toEqual(bob)
  })

  it('obsolete registration 401 does not cancel newer queued login', async () => {
    const auth = signedIn(), registration = beginRegister(auth, alice)
    const failure = expect(registration).rejects.toBeInstanceOf(ApiError)
    const login = beginLogin(auth, bob)
    await answer('/api/auth/register', { error: { code: 'UNAUTHORIZED' } }, 401); await failure
    await answer('/api/auth/login', { user: bob }); await login
    expect(auth.user).toEqual(bob); expect(serverCookie?.user).toEqual(bob)
  })

  it('recovers queue after failed login so subsequent registration can succeed', async () => {
    const auth = useAuthStore(), login = beginLogin(auth, alice)
    const failure = expect(login).rejects.toBeInstanceOf(ApiError)
    const registration = beginRegister(auth, bob)
    await answer('/api/auth/login', { error: { code: 'INVALID_CREDENTIALS' } }, 401); await failure
    await answer('/api/auth/register', { user: bob }); await registration
    expect(auth.user).toEqual(bob); expect(serverCookie?.user).toEqual(bob)
  })
  it('ignores old ordinary API 401 and applies current session 401', async () => {
    const auth = signedIn(), oldRequest = api('/api/channels')
    await loginAs(auth, bob)
    await answer('/api/channels', { error: { code: 'UNAUTHORIZED' } }, 401)
    await expect(oldRequest).rejects.toBeInstanceOf(ApiError); expect(auth.user).toEqual(bob)
    const current = api('/api/members')
    await answer('/api/members', { error: { code: 'UNAUTHORIZED' } }, 401)
    await expect(current).rejects.toBeInstanceOf(ApiError); expect(auth.user).toBeNull()
  })
  it('rotates callback generation after password cookie/token refresh', async () => {
    const auth = signedIn(), old = api('/api/channels'), previousVersion = serverCookie?.version
    const password = auth.changePassword('old-synthetic-password', 'new-synthetic-password')
    await answer('/api/auth/password', null, 204); await password
    expect(serverCookie?.version).toBeGreaterThan(previousVersion ?? 0)
    await answer('/api/channels', { error: { code: 'UNAUTHORIZED' } }, 401)
    await expect(old).rejects.toBeInstanceOf(ApiError); expect(auth.user).toEqual(alice)
    const fresh = auth.checkAuth(); await answer('/api/auth/me', serverCookie?.user)
    expect(await fresh).toBe(true)
  })
  it.each([204, 401])('orders password %s, logout and newest login with correct cookie', async status => {
    const auth = signedIn(), password = auth.changePassword('old-synthetic-password', 'new-synthetic-password')
    const outcome = status === 204 ? password : expect(password).rejects.toBeInstanceOf(ApiError)
    await started('/api/auth/password')
    const logout = auth.logout(), login = beginLogin(auth, bob)
    await started('/api/auth/password'); expect(requests.some(item => item.path === '/api/auth/logout')).toBe(false)
    await answer('/api/auth/password', null, status); await outcome
    await answer('/api/auth/logout', null, 204); await logout
    await answer('/api/auth/login', { user: bob }); await login
    expect(auth.user).toEqual(bob); expect(serverCookie?.user).toEqual(bob)
  })
  it.each([false, true])('does not target replacement login cookie with queued password (existing account: %s)', async existing => {
    const auth = existing ? signedIn() : useAuthStore()
    const login = beginLogin(auth, bob)
    await started('/api/auth/login')
    const password = auth.changePassword('old-synthetic-password', 'new-synthetic-password')
    const rejection = expect(password).rejects.toMatchObject({ status: 401, code: 'UNAUTHORIZED' })
    await answer('/api/auth/login', { user: bob }); await login; await rejection
    expect(requests.some(item => item.path === '/api/auth/password')).toBe(false)
    expect(auth.user).toEqual(bob); expect(serverCookie?.user).toEqual(bob)
  })

  it('current password 401 still invalidates current account', async () => {
    const auth = signedIn(), password = auth.changePassword('old-synthetic-password', 'new-synthetic-password')
    const failure = expect(password).rejects.toBeInstanceOf(ApiError)
    await answer('/api/auth/password', { error: { code: 'UNAUTHORIZED' } }, 401); await failure
    expect(auth.user).toBeNull()
  })
})

describe('account mutation races', () => {
  const mutations = ['profile', 'avatar', 'locale'] as const
  function mutate(auth: ReturnType<typeof useAuthStore>, operation: typeof mutations[number]) {
    if (operation === 'profile') return auth.updateProfile({ displayName: 'Changed Alice', bio: 'synthetic' })
    if (operation === 'avatar') return auth.uploadAvatar(new File(['synthetic'], 'avatar.png', { type: 'image/png' }))
    return auth.changeLocale('en')
  }
  it.each(mutations)('does not revive old %s after logout and loginB', async operation => {
    const auth = signedIn(), mutation = mutate(auth, operation), logout = auth.logout()
    await answer('/api/auth/logout', null, 204); await logout; await loginAs(auth, bob)
    await answer(`/api/users/me/${operation}`, { ...alice, display_name: 'Old Alice', locale: 'de' }); await mutation
    expect(auth.user).toEqual(bob); expect(locale.value).toBe('en')
  })
  it.each(mutations)('keeps newest same-account session after old %s', async operation => {
    const auth = signedIn(), mutation = mutate(auth, operation), latest = { ...alice, display_name: 'Newest Alice' }
    await loginAs(auth, latest)
    await answer(`/api/users/me/${operation}`, { ...alice, display_name: 'Old Alice' }); await mutation
    expect(auth.user).toEqual(latest)
  })
  it.each(['profile', 'avatar'] as const)('ignores wrong-account %s response', async operation => {
    const auth = signedIn(), mutation = mutate(auth, operation)
    await answer(`/api/users/me/${operation}`, bob); await mutation; expect(auth.user).toEqual(alice)
  })
  it('does not restore old locale after account-changing failed locale request', async () => {
    const auth = signedIn(), mutation = auth.changeLocale('en')
    await loginAs(auth, bob); request('/api/users/me/locale').reject(new TypeError('offline'))
    await expect(mutation).rejects.toBeInstanceOf(ApiError); expect(auth.user).toEqual(bob); expect(locale.value).toBe('en')
  })
  it('does not roll back newer locale choice when old choice fails', async () => {
    const auth = signedIn(), older = auth.changeLocale('en'), newer = auth.changeLocale('en')
    await answer('/api/users/me/locale', { ...alice, locale: 'en' }, 200, 1); await newer
    request('/api/users/me/locale').reject(new TypeError('offline')); await expect(older).rejects.toBeInstanceOf(ApiError)
    expect(auth.user?.locale).toBe('en'); expect(locale.value).toBe('en')
  })
  it('ignores older successful locale persistence after newer choice', async () => {
    const auth = signedIn(), older = auth.changeLocale('en'), newer = auth.changeLocale('de')
    await answer('/api/users/me/locale', alice, 200, 1); await newer
    await answer('/api/users/me/locale', { ...alice, locale: 'en' }); await older
    expect(auth.user?.locale).toBe('de'); expect(locale.value).toBe('de')
  })
  it('does not let automatic locale persistence overwrite subsequent login', async () => {
    const auth = useAuthStore(), checking = auth.checkAuth()
    await answer('/api/auth/me', { ...alice, locale: '' }); expect(await checking).toBe(true)
    await started('/api/users/me/locale'); await loginAs(auth, bob)
    await answer('/api/users/me/locale', alice); await vi.waitFor(() => expect(auth.user).toEqual(bob))
    expect(locale.value).toBe('en')
  })
})
