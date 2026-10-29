import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import MarkdownHelp from './MarkdownHelp.vue'
import { renderMarkdown } from '../lib/markdown'
import { setLocale } from '../i18n'

enableAutoUnmount(afterEach)
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

const mountHelp = (props = {}) => mount(MarkdownHelp, { props, attachTo: document.body })
const button = (w: ReturnType<typeof mountHelp>) => w.find('[data-testid="markdown-help-button"]')
const openHelp = async (w: ReturnType<typeof mountHelp>) => {
  await button(w).trigger('click')
  return w.find('[role="dialog"]')
}

describe('MarkdownHelp', () => {
  it('shows what to type next to how it looks, grouped, in columns', async () => {
    const dialog = await openHelp(mountHelp())
    expect(dialog.findAll('section h3').map(h => h.text())).toEqual(['Text', 'Blocks', 'Links and mentions'])
    expect(dialog.text()).toContain('Type')
    expect(dialog.text()).toContain('Result')
    expect(dialog.find('code').text()).toBe('**bold**')
    expect(dialog.find('strong').text()).toBe('bold')
    expect(dialog.find('.md-spoiler').exists()).toBe(true)
    expect(dialog.find('h1').text()).toBe('Heading')
    expect(dialog.find('.md-codeblock').exists()).toBe(true)
    expect(dialog.find('.md-mention-group').text()).toBe('@all')
  })

  it('renders every example with the message renderer', async () => {
    const dialog = await openHelp(mountHelp())
    const examples = dialog.findAll('[data-testid="format-example"]')
    const results = dialog.findAll('[data-testid="format-result"]')
    expect(examples.length).toBe(16)
    expect(results.length).toBe(examples.length)
    examples.forEach((example, i) => {
      const source = example.find('code').text()
      const expected = document.createElement('div')
      expected.innerHTML = renderMarkdown(source, { known: new Set() })
      expect(results[i]?.find('.markdown-body').element.innerHTML).toBe(expected.innerHTML)
    })
  })

  it('never starts link previews from its examples', async () => {
    const w = mountHelp()
    await openHelp(w)
    expect(w.findComponent({ name: 'LinkPreviewCard' }).exists()).toBe(false)
  })

  it('hands an example to the composer and closes', async () => {
    const w = mountHelp()
    const dialog = await openHelp(w)
    await dialog.find('[data-testid="format-example"]').trigger('click')
    expect(w.emitted('insert')).toEqual([['**bold**', { block: false }]])
    expect(w.find('[role="dialog"]').exists()).toBe(false)
  })

  it('marks block examples and leaves a space after the mention', async () => {
    const w = mountHelp()
    const dialog = await openHelp(w)
    const examples = dialog.findAll('[data-testid="format-example"]')
    await examples[6]!.trigger('click')
    await openHelp(w)
    await w.findAll('[data-testid="format-example"]')[15]!.trigger('click')
    expect(w.emitted('insert')).toEqual([['```js\nlet a = 1\n```', { block: true }], ['@all ', { block: false }]])
  })

  it('closes with Escape, the close button and a click elsewhere, and hands focus back', async () => {
    const w = mountHelp()
    await openHelp(w)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await w.vm.$nextTick()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button(w).element)
    await openHelp(w)
    await w.find('[role="dialog"] button').trigger('click')
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    await openHelp(w)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
  })

  it('stays open for clicks inside', async () => {
    const w = mountHelp()
    const dialog = await openHelp(w)
    dialog.element.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.find('[role="dialog"]').exists()).toBe(true)
  })

  it('can be disabled with the composer', () => {
    expect(button(mountHelp({ disabled: true })).attributes('disabled')).toBeDefined()
  })
})
