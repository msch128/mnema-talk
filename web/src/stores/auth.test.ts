import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './auth'
import { locale, setLocale } from '../i18n'
import { userFixture } from '../test-fixtures.fixture'
import type { User } from '../types/domain'

function respond(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
}

function stubFetch(me: User) {
  const calls: [string, RequestInit & { body: string }][] = []
  const fn = vi.fn((url: RequestInfo | URL, init: RequestInit = {}) => {
    const options = { ...init, body: typeof init.body === 'string' ? init.body : '' }
    calls.push([String(url), options])
    if (url === '/api/auth/me') return respond(me)
    if (url === '/api/users/me/locale') return respond({ ...me, locale: JSON.parse(options.body).locale })
    return respond(null, 204)
  })
  vi.stubGlobal('fetch', fn)
  return calls
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('de')
})
afterEach(() => vi.unstubAllGlobals())

describe('auth locale', () => {
  it('uses the stored account language on session restore', async () => {
    const calls = stubFetch(userFixture({ locale: 'en' }))
    await useAuthStore().checkAuth()
    expect(locale.value).toBe('en')
    expect(calls.some(([u]) => u === '/api/users/me/locale')).toBe(false)
  })

  it('picks the browser language when none is stored and saves it', async () => {
    vi.stubGlobal('navigator', { language: 'en-GB' })
    const calls = stubFetch(userFixture({ locale: '' }))
    const auth = useAuthStore()
    await auth.checkAuth()
    expect(locale.value).toBe('en')
    await vi.waitFor(() => expect(auth.user?.locale).toBe('en'))
    const put = calls.find(([u]) => u === '/api/users/me/locale')
    expect(put?.[1].method).toBe('PUT')
    expect(JSON.parse(put?.[1].body ?? '{}')).toEqual({ locale: 'en' })
  })

  it('defaults to German for other browser languages', async () => {
    vi.stubGlobal('navigator', { language: 'fr-FR' })
    const calls = stubFetch(userFixture({ locale: '' }))
    setLocale('en')
    await useAuthStore().checkAuth()
    expect(locale.value).toBe('de')
    expect(JSON.parse(calls.find(([u]) => u === '/api/users/me/locale')?.[1].body ?? '{}')).toEqual({ locale: 'de' })
  })

  it('switches and stores the language from the account menu', async () => {
    const calls = stubFetch(userFixture({ locale: 'de' }))
    const auth = useAuthStore()
    await auth.checkAuth()
    await auth.changeLocale('en')
    expect(locale.value).toBe('en')
    expect(auth.user?.locale).toBe('en')
    expect(JSON.parse(calls.at(-1)?.[1].body ?? '{}')).toEqual({ locale: 'en' })
  })
})

describe('auth locale chosen before login', () => {
  it('beats the stored language and is saved', async () => {
    const { chooseLocale } = await import('../i18n')
    chooseLocale('en')
    const calls = stubFetch(userFixture({ locale: 'de' }))
    const auth = useAuthStore()
    await auth.checkAuth()
    expect(locale.value).toBe('en')
    await vi.waitFor(() => expect(auth.user?.locale).toBe('en'))
    expect(JSON.parse(calls.find(([u]) => u === '/api/users/me/locale')?.[1].body ?? '{}')).toEqual({ locale: 'en' })
  })
})

describe('authentication and account mutations', () => {
  afterEach(() => { vi.restoreAllMocks(); localStorage.clear() })

  it('registers through an invite, updates account fields and uploads an avatar', async () => {
    const member = userFixture({ locale: 'de' })
    const requests: Array<{ path: string; init: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn((url: RequestInfo | URL, init: RequestInit = {}) => {
      requests.push({ path: String(url), init })
      if (String(url).startsWith('/api/auth/')) {
        if (url === '/api/auth/register') return respond({ user: member })
        return respond(null, 204)
      }
      return respond({ ...member, display_name: 'New name', bio: 'New bio' })
    }))
    const auth = useAuthStore()
    expect(auth.isAuthenticated).toBe(false)
    expect(auth.isAdmin).toBe(false)
    expect(await auth.register('member', 'Name', 'test-only-password', 'synthetic-invite')).toEqual(member)
    expect(auth.isAuthenticated).toBe(true)
    expect(JSON.parse(String(requests[0]?.init.body))).toEqual({ username: 'member', display_name: 'Name', password: 'test-only-password', invite_code: 'synthetic-invite' })
    await auth.updateProfile({ displayName: 'New name', bio: 'New bio' })
    expect(auth.user?.display_name).toBe('New name')
    expect(JSON.parse(String(requests.find(r => r.path === '/api/users/me/profile')?.init.body))).toEqual({ display_name: 'New name', bio: 'New bio' })
    const file = new File(['synthetic'], 'avatar.png', { type: 'image/png' })
    await auth.uploadAvatar(file)
    expect(requests.find(r => r.path === '/api/users/me/avatar')?.init.body).toBeInstanceOf(FormData)
    const form = requests.find(r => r.path === '/api/users/me/avatar')?.init.body
    if (!(form instanceof FormData)) throw new Error('Expected avatar multipart form')
    expect(form.get('avatar')).toEqual(file)
    await auth.changePassword('old-test-password', 'new-test-password')
    expect(JSON.parse(String(requests.find(r => r.path === '/api/auth/password')?.init.body))).toEqual({ current_password: 'old-test-password', new_password: 'new-test-password' })
    await auth.logout()
    expect(auth.user).toBeNull()
  })

  it('logs in an administrator and clears the session on unauthorized responses', async () => {
    const member = userFixture({ role: 'admin', locale: 'de' })
    const fetch = vi.fn().mockImplementation(() => respond({ user: member }))
    vi.stubGlobal('fetch', fetch)
    const auth = useAuthStore()
    expect(await auth.login('member', 'test-only-password')).toEqual(member)
    expect(auth.isAdmin).toBe(true)
    fetch.mockImplementation(() => respond({ error: 'expired' }, 401))
    expect(await auth.checkAuth()).toBe(false)
    expect(auth.isAuthenticated).toBe(false)
  })

  it('retains the current account during transient outages and clears it if logout fails', async () => {
    const auth = useAuthStore()
    auth.user = userFixture()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    expect(await auth.checkAuth()).toBeNull()
    expect(auth.user?.username).toBe('member')
    await expect(auth.logout()).rejects.toThrow()
    expect(auth.user).toBeNull()
  })

  it('rolls a failed language change back to the valid account language', async () => {
    const auth = useAuthStore()
    auth.user = userFixture({ locale: 'de' })
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    await expect(auth.changeLocale('en')).rejects.toThrow()
    expect(locale.value).toBe('de')
    auth.user = userFixture({ locale: '' })
    await expect(auth.changeLocale('en')).rejects.toThrow()
    expect(locale.value).toBe('en')
  })

  it('tolerates blocked storage and a failed initial locale persistence', async () => {
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked') })
    const fetch = vi.fn((url: RequestInfo | URL) => url === '/api/auth/me' ? respond(userFixture({ locale: '' })) : respond({ error: 'offline' }, 503))
    vi.stubGlobal('fetch', fetch)
    const auth = useAuthStore()
    expect(await auth.checkAuth()).toBe(true)
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    expect(auth.isAuthenticated).toBe(true)
  })
})

describe('runtime account input validation', () => {
  it('ignores unsupported language requests without changing the account or calling the server', async () => {
    const auth = useAuthStore()
    auth.user = userFixture({ locale: 'de' })
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await Reflect.apply(auth.changeLocale, auth, ['unsupported'])
    expect(locale.value).toBe('de')
    expect(auth.user.locale).toBe('de')
    expect(fetch).not.toHaveBeenCalled()
  })
})
