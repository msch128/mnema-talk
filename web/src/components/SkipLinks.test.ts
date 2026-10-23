import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SkipLinks from './SkipLinks.vue'
import { i18nPlugin, setLocale } from '../i18n'

afterEach(() => {
  document.body.replaceChildren()
  setLocale('de')
})

describe('SkipLinks', () => {
  it('are the first Tab stops, hidden until focused, and move focus to the messages and the message box', async () => {
    setLocale('en')
    document.body.insertAdjacentHTML('beforeend', '<main id="main-content" tabindex="-1"><textarea data-composer-input></textarea></main>')
    const w = mount(SkipLinks, { attachTo: document.body, global: { plugins: [i18nPlugin] } })
    const box = w.get('[data-testid="skip-links"]')
    expect(box.classes()).toContain('sr-only')
    expect(box.classes()).toContain('focus-within:not-sr-only')

    const buttons = w.findAll('button')
    expect(buttons.map(b => b.text())).toEqual(['Skip to messages', 'Skip to message box'])

    await buttons[0]!.trigger('click')
    expect(document.activeElement?.id).toBe('main-content')
    await buttons[1]!.trigger('click')
    expect(document.activeElement?.hasAttribute('data-composer-input')).toBe(true)
    w.unmount()
  })

  it('do nothing when the target is not on the page', async () => {
    setLocale('en')
    const w = mount(SkipLinks, { attachTo: document.body, global: { plugins: [i18nPlugin] } })
    await w.findAll('button')[0]!.trigger('click')
    await w.findAll('button')[1]!.trigger('click')
    w.unmount()
  })
})
