import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import NavDrawerButton from './NavDrawerButton.vue'
import { useCompactLayout } from '../composables/useCompactLayout'
import { setLocale, t } from '../i18n'

beforeEach(() => {
  setLocale('en')
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener: vi.fn() }))
  useCompactLayout().closeNav()
})
afterEach(() => { vi.unstubAllGlobals() })

it('is a labelled button that is only shown on phones', () => {
  const button = mount(NavDrawerButton).get('[data-testid="open-nav"]')
  expect(button.attributes('aria-label')).toBe(t('sidebar.openChannels'))
  expect(button.classes()).toEqual(expect.arrayContaining(['hidden', 'max-md:flex']))
})

it('opens the channel drawer when pressed', async () => {
  const { navOpen } = useCompactLayout()
  expect(navOpen.value).toBe(false)
  await mount(NavDrawerButton).get('[data-testid="open-nav"]').trigger('click')
  expect(navOpen.value).toBe(true)
})
