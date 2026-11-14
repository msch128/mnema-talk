import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import InviteInactive from './InviteInactive.vue'
import { setLocale } from '../../i18n'
import de from '../../i18n/de.json'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('InviteInactive', () => {
  it('renders the inactive message and a link back to sign-in', () => {
    const w = mount(InviteInactive)
    expect(w.get('[data-testid="invite-inactive-title"]').text()).toBe('This link is not active')
    expect(w.get('[data-testid="invite-inactive-hint"]').text()).toContain('Ask whoever shared it')
    const link = w.get('[data-testid="invite-inactive-signin"]')
    expect(link.attributes('href')).toBe('/')
    expect(link.text()).toBe('Go to sign-in')
  })

  it('switches to German when chosen', async () => {
    const w = mount(InviteInactive)
    const deBtn = w.findAll('button').find(b => b.attributes('lang') === 'de')
    expect(deBtn).toBeDefined()
    await deBtn!.trigger('click')
    expect(w.get('[data-testid="invite-inactive-title"]').text()).toBe(de.login.inviteInactiveTitle)
    expect(w.get('[data-testid="invite-inactive-hint"]').text()).toBe(de.login.inviteInactiveHint)
    expect(w.get('[data-testid="invite-inactive-signin"]').text()).toBe(de.login.inviteInactiveSignIn)
  })
})
