import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import TipBar from './TipBar.vue'
import { useOnboardingStore } from '../stores/onboarding'
import { useVoiceStore } from '../stores/voice'
import { searchOpen } from '../lib/globalSearch'
import { setLocale } from '../i18n'
import { onboardingStateFixture } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)

let dismissed: string[]

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  searchOpen.value = false
  dismissed = []
  const onboarding = useOnboardingStore()
  onboarding.ensureLoaded = vi.fn().mockResolvedValue(undefined)
  onboarding.dismiss = vi.fn(async (id: string) => { dismissed.push(id) })
  onboarding.state = onboardingStateFixture({ welcome_done: true, dismissed_tips: [] })
})

const mountBar = () => mount(TipBar, { attachTo: document.body, global: { directives: { tooltip: () => undefined } } })

function typeInto(attrs: Record<string, string>, data: string) {
  const field = document.createElement('textarea')
  for (const [key, value] of Object.entries(attrs)) field.setAttribute(key, value)
  document.body.append(field)
  field.dispatchEvent(new InputEvent('input', { bubbles: true, data }))
  field.remove()
}

describe('TipBar', () => {
  it('shows the first tip and dismisses it on request', async () => {
    const w = mountBar()
    expect(w.get('[data-testid="tip-bar"]').text()).toContain('Ctrl or Cmd + K')
    await w.get('[data-testid="tip-dismiss"]').trigger('click')
    expect(dismissed).toEqual(['search'])
  })

  it('has no tip before the welcome flow is done', () => {
    useOnboardingStore().state = onboardingStateFixture({ welcome_done: false, dismissed_tips: [] })
    expect(mountBar().find('[data-testid="tip-bar"]').exists()).toBe(false)
  })

  it('retires the search tip once the search has been opened', async () => {
    mountBar()
    searchOpen.value = true
    await nextTick()
    expect(dismissed).toEqual(['search'])
    searchOpen.value = false
    await nextTick()
    expect(dismissed).toEqual(['search'])
  })

  it('retires the mention tip once @ or / is typed in the composer, and not for other typing', async () => {
    useOnboardingStore().state = onboardingStateFixture({ welcome_done: true, dismissed_tips: ['search'] })
    mountBar()
    typeInto({ 'data-composer-input': '' }, 'a')
    typeInto({}, '@')
    expect(dismissed).toEqual([])
    typeInto({ 'data-composer-input': '' }, '@')
    expect(dismissed).toEqual(['mention'])
  })

  it('retires the Talk tip once a Talk is joined, but only while it is the tip on show', async () => {
    mountBar()
    useVoiceStore().currentChannelId = 'talk-1'
    await nextTick()
    expect(dismissed).toEqual([])

    useOnboardingStore().state = onboardingStateFixture({ welcome_done: true, dismissed_tips: ['search', 'mention'] })
    await nextTick()
    useVoiceStore().currentChannelId = 'talk-2'
    await nextTick()
    expect(dismissed).toEqual(['talk'])
  })
})
