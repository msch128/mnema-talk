import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import MessageRow from './MessageRow.vue'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'
import { messageFixture, userFixture } from '../test-fixtures.fixture'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ id: 'me' })
})

const mountRow = (props: Record<string, unknown> = {}) => mount(MessageRow, {
  props: { msg: messageFixture({ id: 'm1' }), ...props },
  global: { directives: { tooltip: () => undefined } }
})

describe('MessageRow keyboard focus', () => {
  it('is a Tab stop by default (a thread or Talk chat has no roving list)', () => {
    expect(mountRow().attributes('tabindex')).toBe('0')
  })

  it('leaves the Tab order when the list has another stop, but stays focusable for the arrow keys', () => {
    const row = mountRow({ tabStop: false })
    expect(row.attributes('tabindex')).toBe('-1')
  })

  it('shows keyboard focus as the shared ring', () => {
    expect(mountRow().classes()).toContain('focus-ring')
  })
})

describe('MessageRow action bar', () => {
  it('is marked, so only one bar shows at a time, and docks above the row on a phone', () => {
    const bar = mountRow().get('[data-msg-toolbar]')
    expect(bar.attributes('data-open')).toBeUndefined()
    expect(bar.classes()).toContain('max-md:-top-14')
  })

  it('stays open while its reaction picker is open', () => {
    const bar = mountRow({ pickerId: 'm1' }).get('[data-msg-toolbar]')
    expect(bar.attributes('data-open')).toBeDefined()
    expect(bar.classes()).toContain('flex')
  })
})
