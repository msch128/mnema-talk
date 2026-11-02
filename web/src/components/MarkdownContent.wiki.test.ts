import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MarkdownContent from './MarkdownContent.vue'
import { currentRoute, navigate } from '../lib/router'
import { summary } from '../wiki.fixture'
import { MAX_DOCUMENT_CHARS, MAX_MARKDOWN_CHARS } from '../lib/markdown'

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  navigate('/', { replace: true })
})

const pages = [summary('1', 'Raid guide')]

describe('MarkdownContent in a wiki page', () => {
  it('turns [[Page]] into a link that opens the page, by click and by Enter', async () => {
    const w = mount(MarkdownContent, { props: { content: 'See [[Raid guide]].', wikiPages: pages, previews: false } })
    const link = w.get('.md-wikilink[data-wiki="raid-guide"]')
    await link.trigger('click')
    expect(currentRoute.value).toMatchObject({ view: 'wiki', wiki: { mode: 'page', slug: 'raid-guide' } })
    navigate('/', { replace: true })
    await link.trigger('keydown', { key: 'Enter' })
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'raid-guide' } })
  })

  it('leaves a link to a missing page as text that goes nowhere', async () => {
    const w = mount(MarkdownContent, { props: { content: '[[Nowhere]]', wikiPages: pages, previews: false } })
    await w.get('.md-wikilink-missing').trigger('click')
    expect(currentRoute.value.view).not.toBe('wiki')
  })

  it('reads [[brackets]] in a message as plain text', () => {
    const w = mount(MarkdownContent, { props: { content: 'See [[Raid guide]].', previews: false } })
    expect(w.find('.md-wikilink').exists()).toBe(false)
    expect(w.text()).toContain('[[Raid guide]]')
  })

  it('renders a page longer than a message is allowed to be', () => {
    const long = `${'word '.repeat(MAX_MARKDOWN_CHARS / 4)}**END**`
    expect(long.length).toBeGreaterThan(MAX_MARKDOWN_CHARS)
    expect(long.length).toBeLessThan(MAX_DOCUMENT_CHARS)
    const page = mount(MarkdownContent, { props: { content: long, wikiPages: pages, previews: false } })
    const message = mount(MarkdownContent, { props: { content: long, previews: false } })
    expect(page.find('strong').text()).toBe('END')
    expect(message.find('strong').exists()).toBe(false)
  })
})
