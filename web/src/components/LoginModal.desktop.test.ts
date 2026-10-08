// Context5 successor to unchanged approved Context4 fixture630ebd17.
// Explicit null is the initial native auth comparator, never an auth grant.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LoginModal from './LoginModal.vue'
import { initializeNativeContext, installNativePort } from '../lib/nativeTransport'
import { setLocale } from '../i18n'

const context = '00000000-0000-4000-8000-000000000001'
const dummyUsername = 'fixture_user'
const dummyPassword = 'x'.repeat(31)
let expectedUsername = dummyUsername
let expectedPassword = dummyPassword
let capture: { usernameLength: number; passwordBytes: number; usernameMatches: boolean; passwordMatches: boolean; authenticationMatches: boolean } | null
let wrapper: VueWrapper | null = null
beforeEach(async () => {
  vi.stubGlobal('isTauri', true); setActivePinia(createPinia()); setLocale('en'); capture = null
  expectedUsername = dummyUsername; expectedPassword = dummyPassword
  installNativePort({
    channel: () => ({ onmessage: () => {} }),
    invoke: async (command, args) => {
      if (command === 'native_context') return { context, profile_intent: '00000000-0000-4000-8000-000000000002', content_authorization: 'unavailable', remembered_login: false, authentication_intent: null }
      if (command === 'native_auth_login') {
        const username = args?.['username']; const password = args?.['password']
        capture = {
          usernameLength: typeof username === 'string' ? username.length : -1,
          passwordBytes: typeof password === 'string' ? new TextEncoder().encode(password).length : -1,
          usernameMatches: username === expectedUsername,
          passwordMatches: password === expectedPassword,
          authenticationMatches: args?.['authenticationIntent'] === null,
        }
        return { context, status: 401, body: { error: { code: 'INVALID_CREDENTIALS' } } }
      }
      return { context, status: 503, body: { error: { code: 'UNAVAILABLE' } } }
    },
  })
  await initializeNativeContext()
  wrapper = mount(LoginModal, { attachTo: document.body, global: { stubs: { LegalModal: true } } })
})
afterEach(() => { vi.unstubAllGlobals(); wrapper?.unmount(); wrapper = null; window.history.replaceState(null, '', '/') })

async function submitDom(username: string, password: string) {
  if (!wrapper) throw new Error('fixture not mounted')
  wrapper.get<HTMLInputElement>('#login-username').element.value = username
  wrapper.get<HTMLInputElement>('#login-password').element.value = password
  // AX/autofill need not dispatch input/change. Exercise the registered form
  // callback rather than writing Vue refs or calling handleSubmit directly.
  wrapper.get('form').element.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flushPromises()
}
function matches() {
  expect(capture !== null).toBe(true)
  expect(capture?.usernameMatches).toBe(true)
  expect(capture?.passwordMatches).toBe(true)
  expect(capture?.authenticationMatches).toBe(true)
}
describe('actual LoginModal DOM to Context5 native command', () => {
  it('submits password-manager fields with no input events', async () => {
    await submitDom(expectedUsername, expectedPassword)
    matches(); expect(capture?.passwordBytes).toBe(31)
    // A failed native reply causes a render. It must not restore stale empty
    // v-model fields before the next explicit retry.
    expect(wrapper!.get<HTMLInputElement>('#login-username').element.value === expectedUsername).toBe(true)
    expect(wrapper!.get<HTMLInputElement>('#login-password').element.value === expectedPassword).toBe(true)
    await wrapper!.get('form').trigger('submit'); await flushPromises()
    matches()
  })
  it('submits current DOM fields instead of earlier Vue model values', async () => {
    await wrapper!.get('#login-username').setValue('earlier_fixture')
    await wrapper!.get('#login-password').setValue('earlier_fixture_password')
    await submitDom(expectedUsername, expectedPassword)
    matches()
  })
  it('preserves exact displayed password spaces and Unicode bytes', async () => {
    expectedUsername = ' spaced_fixture '
    expectedPassword = '  é🔒fixture  '
    await submitDom(expectedUsername, expectedPassword)
    matches(); expect(capture?.passwordBytes).toBe(new TextEncoder().encode(expectedPassword).length)
  })
  it.each([71, 72, 73])('does not trim or truncate a %i-byte password before the native validator', async size => {
    expectedPassword = 'x'.repeat(size)
    await submitDom(expectedUsername, expectedPassword)
    matches(); expect(capture?.passwordBytes).toBe(size)
  })
})
