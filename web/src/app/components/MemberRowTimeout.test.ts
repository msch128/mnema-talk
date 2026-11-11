import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MemberRow from './MemberRow.vue'
import { usePermissionsStore } from '../../stores/permissions'
import { PERMISSION_BITS } from '../../shared/lib/permissions'
import { setLocale } from '../../i18n'
import { userFixture } from '../../test-fixtures.fixture'

const future = () => new Date(Date.now() + 3_600_000).toISOString()

function allow(bits: number) {
  usePermissionsStore().info = { owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
}

function row(timeoutUntil?: string) {
  const member = { ...userFixture({ username: 'ada' }), ...(timeoutUntil ? { timeout_until: timeoutUntil } : {}) }
  return mount(MemberRow, { props: { member } })
}

enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers() })

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})

describe('member list timeout indicator', () => {
  it('shows a clock that says until when, to those who may time members out', () => {
    allow(PERMISSION_BITS.timeout_members)
    const clock = row(future()).get('[data-testid="member-timed-out"]')
    expect(clock.attributes('aria-label')).toMatch(/^Timed out until /)
  })

  it('shows nothing once the timeout has run out or without one', () => {
    allow(PERMISSION_BITS.timeout_members)
    expect(row(new Date(Date.now() - 1000).toISOString()).find('[data-testid="member-timed-out"]').exists()).toBe(false)
    expect(row().find('[data-testid="member-timed-out"]').exists()).toBe(false)
  })

  it('keeps a timeout private from members who cannot moderate', () => {
    allow(0)
    expect(row(future()).find('[data-testid="member-timed-out"]').exists()).toBe(false)
  })

  it('drops the clock by itself when the timeout runs out', async () => {
    vi.useFakeTimers()
    allow(PERMISSION_BITS.timeout_members)
    const w = row(new Date(Date.now() + 5000).toISOString())
    expect(w.find('[data-testid="member-timed-out"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(w.find('[data-testid="member-timed-out"]').exists()).toBe(false)
    w.unmount()
  })
})
