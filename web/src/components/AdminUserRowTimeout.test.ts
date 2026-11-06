import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminUserRow from './AdminUserRow.vue'
import { setLocale } from '../i18n'
import { member } from './adminTestkit.fixture'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

function status(timeoutUntil?: string) {
  const user = { ...member(), ...(timeoutUntil ? { timeout_until: timeoutUntil } : {}) }
  const w = mount(AdminUserRow, { props: { user }, global: { stubs: { UserAvatar: true } } })
  return w.get('[data-testid="user-status"]').text()
}

describe('admin user row status', () => {
  it('shows a running timeout instead of Active', () => {
    const until = new Date(Date.now() + 3_600_000).toISOString()
    expect(status(until)).toMatch(/^Timed out until /)
  })

  it('is Active again once the timeout has run out', () => {
    expect(status(new Date(Date.now() - 1000).toISOString())).toBe('Active')
    expect(status()).toBe('Active')
  })
})
