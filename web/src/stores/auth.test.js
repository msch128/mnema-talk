import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './auth'
import { locale, setLocale } from '../i18n'

function respond(body, status = 200) {
  return Promise.resolve({ status, ok: status < 400, json: () => Promise.resolve(body) })
}

function stubFetch(me) {
  const calls = []
  const fn = vi.fn((url, init) => {
    calls.push([url, init])
    if (url === '/api/auth/me') return respond(me)
    if (url === '/api/users/me/locale') return respond({ ...me, locale: JSON.parse(init.body).locale })
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
    const calls = stubFetch({ id: 'u1', locale: 'en' })
    await useAuthStore().checkAuth()
    expect(locale.value).toBe('en')
    expect(calls.some(([u]) => u === '/api/users/me/locale')).toBe(false)
  })

  it('picks the browser language when none is stored and saves it', async () => {
    vi.stubGlobal('navigator', { language: 'en-GB' })
    const calls = stubFetch({ id: 'u1', locale: '' })
    const auth = useAuthStore()
    await auth.checkAuth()
    expect(locale.value).toBe('en')
    await vi.waitFor(() => expect(auth.user.locale).toBe('en'))
    const put = calls.find(([u]) => u === '/api/users/me/locale')
    expect(put[1].method).toBe('PUT')
    expect(JSON.parse(put[1].body)).toEqual({ locale: 'en' })
  })

  it('defaults to German for other browser languages', async () => {
    vi.stubGlobal('navigator', { language: 'fr-FR' })
    const calls = stubFetch({ id: 'u1', locale: '' })
    setLocale('en')
    await useAuthStore().checkAuth()
    expect(locale.value).toBe('de')
    expect(JSON.parse(calls.find(([u]) => u === '/api/users/me/locale')[1].body)).toEqual({ locale: 'de' })
  })

  it('switches and stores the language from the account menu', async () => {
    const calls = stubFetch({ id: 'u1', locale: 'de' })
    const auth = useAuthStore()
    await auth.checkAuth()
    await auth.changeLocale('en')
    expect(locale.value).toBe('en')
    expect(auth.user.locale).toBe('en')
    expect(JSON.parse(calls.at(-1)[1].body)).toEqual({ locale: 'en' })
  })
})

describe('auth locale chosen before login', () => {
  it('beats the stored language and is saved', async () => {
    const { chooseLocale } = await import('../i18n')
    chooseLocale('en')
    const calls = stubFetch({ id: 'u1', locale: 'de' })
    const auth = useAuthStore()
    await auth.checkAuth()
    expect(locale.value).toBe('en')
    await vi.waitFor(() => expect(auth.user.locale).toBe('en'))
    expect(JSON.parse(calls.find(([u]) => u === '/api/users/me/locale')[1].body)).toEqual({ locale: 'en' })
  })
})
