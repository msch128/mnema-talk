import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import CommunityRulesDialog from './CommunityRulesDialog.vue'
import SidebarServerMenu from './SidebarServerMenu.vue'
import { useOnboardingStore } from '../stores/onboarding'
import { rulesOpen } from '../lib/communityRules'
import { setLocale } from '../i18n'
import type { OnboardingState } from '../types/rest'

const state = (rules: string): OnboardingState => ({
  community_name: 'Mnema', welcome_text: '', rules_text: rules, rules_version: 1, rules_accepted_version: 1,
  rules_pending: false, welcome_done: true, dismissed_tips: [], start_channel_id: null
} as unknown as OnboardingState)

const mountMenu = () => mount(SidebarServerMenu, { attachTo: document.body, global: { directives: { tooltip: () => undefined } } })

beforeEach(() => { setActivePinia(createPinia()); setLocale('en') })

describe('reading the community rules again', () => {
  it('shows the rules as formatted, read-only text', () => {
    useOnboardingStore().state = state('1. Be **kind**')
    const w = mount(CommunityRulesDialog, { attachTo: document.body })
    expect(document.body.querySelector('[data-testid="community-rules"] strong')?.textContent).toBe('kind')
    w.unmount()
  })

  it('shows an empty dialog while the rules are not loaded', () => {
    const w = mount(CommunityRulesDialog, { attachTo: document.body })
    expect(document.body.querySelector('[data-testid="community-rules"]')?.textContent?.trim()).toBe('')
    w.unmount()
  })

  it('closes with its close button', async () => {
    const onboarding = useOnboardingStore()
    onboarding.state = state('Rule')
    rulesOpen.value = true
    const w = mount(CommunityRulesDialog, { attachTo: document.body })
    document.body.querySelector<HTMLElement>('[data-dialog-close]')?.click()
    expect(rulesOpen.value).toBe(false)
    w.unmount()
  })

  it('offers the rules in the server menu only when there are some, and opens them', async () => {
    const onboarding = useOnboardingStore()
    onboarding.state = state('')
    let w = mountMenu()
    await w.find('[aria-controls="server-menu"]').trigger('click')
    expect(w.find('[data-testid="menu-community-rules"]').exists()).toBe(false)
    w.unmount()

    onboarding.state = state('Be kind')
    w = mountMenu()
    await w.find('[aria-controls="server-menu"]').trigger('click')
    await w.get('[data-testid="menu-community-rules"]').trigger('click')
    expect(rulesOpen.value).toBe(true)
    w.unmount()
  })
})
