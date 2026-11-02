import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiEditor from './WikiEditor.vue'
import { ApiError } from '../lib/api'
import { currentRoute, navigate } from '../lib/router'
import { wikiDirty } from '../lib/wikiNav'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ createWikiPage: vi.fn(), changeWikiPage: vi.fn(), fetchWikiPage: vi.fn(), fetchWikiTree: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)

const STALE = 'the page changed since you opened it; open it again and apply your change to the new version'

function open(page: ReturnType<typeof wikiPage> | null) {
  const w = mount(WikiEditor, { props: { page } })
  return { w, field: (id: string) => w.get(`[data-testid="wiki-${id}"]`) }
}

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  Object.values(api).forEach(fn => fn.mockReset())
  api.fetchWikiTree.mockResolvedValue([])
  grantWiki(VIEW_WIKI | EDIT_WIKI)
  wikiDirty.value = false
  navigate('/wiki/new', { replace: true })
})

describe('creating a page', () => {
  it('sends title, text, address, parent and summary, then opens the page', async () => {
    useWikiStore().pages = [summary('g', 'Guides'), summary('deep', 'Deep', { parent_id: 'g' })]
    api.createWikiPage.mockResolvedValue(wikiPage({ slug: 'raid', title: 'Raid' }))
    const { w, field } = open(null)
    await field('title-input').setValue('Raid')
    await field('text-input').setValue('Body')
    await field('slug-input').setValue('raid')
    await field('parent-input').setValue('g')
    await field('summary-input').setValue('first')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.createWikiPage).toHaveBeenCalledWith({ title: 'Raid', content: 'Body', slug: 'raid', parent_id: 'g', summary: 'first' })
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'raid' } })
    expect(wikiDirty.value).toBe(false)
  })

  it('leaves out an empty address, parent and summary and suggests the address from the title', async () => {
    api.createWikiPage.mockResolvedValue(wikiPage())
    const { w, field } = open(null)
    await field('title-input').setValue('Größe')
    expect(field('slug-input').attributes('placeholder')).toBe('groesse')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.createWikiPage).toHaveBeenCalledWith({ title: 'Größe', content: '' })
  })

  it('does not save without a title', async () => {
    const { w, field } = open(null)
    expect(field('save').attributes('disabled')).toBeDefined()
    await w.get('form').trigger('submit')
    expect(api.createWikiPage).not.toHaveBeenCalled()
  })

  it('shows the server refusal and keeps the text', async () => {
    api.createWikiPage.mockRejectedValue(new ApiError(409, 'CONFLICT', 'that address is already used by a page or a redirect'))
    const { w, field } = open(null)
    await field('title-input').setValue('Raid')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(field('error').text()).toContain('already used')
    expect((field('title-input').element as HTMLInputElement).value).toBe('Raid')
  })
})

describe('editing a page', () => {
  it('knows when the text differs and saves on top of the revision it read', async () => {
    api.changeWikiPage.mockResolvedValue(wikiPage({ rev: 4, content: 'new' }))
    const { w, field } = open(wikiPage())
    expect(field('save').attributes('disabled')).toBeDefined()
    expect(wikiDirty.value).toBe(false)
    await field('text-input').setValue('new')
    expect(wikiDirty.value).toBe(true)
    expect(field('save').attributes('disabled')).toBeUndefined()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.changeWikiPage).toHaveBeenCalledWith('faq', { base_rev: 3, title: 'FAQ', content: 'new' })
    expect(useWikiStore().page?.rev).toBe(4)
    expect(wikiDirty.value).toBe(false)
  })

  it('forgets the unsaved mark when it goes away', async () => {
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('x')
    w.unmount()
    expect(wikiDirty.value).toBe(false)
  })

  it('counts bytes, not characters, and blocks a text over 50 KB', async () => {
    const { field } = open(wikiPage())
    await field('text-input').setValue('ä'.repeat(26 * 1024))
    expect(field('size').text()).toContain('50')
    expect(field('save').attributes('disabled')).toBeDefined()
    await field('text-input').setValue('a'.repeat(1024))
    expect(field('save').attributes('disabled')).toBeUndefined()
  })

  it('shows the difference when somebody saved meanwhile, and nothing is overwritten', async () => {
    api.changeWikiPage.mockRejectedValueOnce(new ApiError(409, 'CONFLICT', STALE))
    api.fetchWikiPage.mockResolvedValue(wikiPage({ rev: 4, content: 'theirs', updated_by: { id: 'u2', username: 'bo', display_name: 'Bo' } }))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(field('conflict').text()).toContain('Bo')
    expect(field('conflict').find('[data-testid="wiki-diff"]').exists()).toBe(true)
    expect(api.changeWikiPage).toHaveBeenCalledTimes(1)
    expect((field('text-input').element as HTMLTextAreaElement).value).toBe('mine')

    api.changeWikiPage.mockResolvedValueOnce(wikiPage({ rev: 5, content: 'mine' }))
    await field('conflict-mine').trigger('click')
    await flushPromises()
    expect(api.changeWikiPage).toHaveBeenLastCalledWith('faq', { base_rev: 4, title: 'FAQ', content: 'mine' })
    expect(useWikiStore().page?.rev).toBe(5)
  })

  it('can take their version instead', async () => {
    api.changeWikiPage.mockRejectedValueOnce(new ApiError(409, 'CONFLICT', STALE))
    api.fetchWikiPage.mockResolvedValue(wikiPage({ rev: 4, content: 'theirs' }))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    await w.get('form').trigger('submit')
    await flushPromises()
    await field('conflict-theirs').trigger('click')
    await flushPromises()
    expect(useWikiStore().page?.content).toBe('theirs')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'faq' } })
  })

  it('shows an ordinary failure, also when the newer page cannot be read', async () => {
    api.changeWikiPage.mockRejectedValue(new ApiError(409, 'CONFLICT', STALE))
    api.fetchWikiPage.mockRejectedValue(new Error('offline'))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-testid="wiki-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="wiki-conflict"]').exists()).toBe(false)
    api.changeWikiPage.mockRejectedValue(new ApiError(403, 'FORBIDDEN', 'this page is locked; only holders of Manage wiki can change it'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(field('error').text()).toContain('locked')
  })
})

describe('the toolbar and the preview', () => {
  it('wraps the selection in Markdown', async () => {
    const { w, field } = open(wikiPage({ content: 'say hello now' }))
    const area = field('text-input').element as HTMLTextAreaElement
    area.setSelectionRange(4, 9)
    await w.get('[data-tool="bold"]').trigger('click')
    await flushPromises()
    expect(area.value).toBe('say **hello** now')
  })

  it('previews the text through the safe renderer, links included', async () => {
    useWikiStore().pages = [summary('1', 'Rules')]
    const { field } = open(null)
    await field('text-input').setValue('See [[Rules]] <script>alert(1)</script>')
    const preview = field('preview')
    expect(preview.find('.md-wikilink[data-wiki="rules"]').exists()).toBe(true)
    expect(preview.find('script').exists()).toBe(false)
  })

  it('switches between writing and preview on a narrow screen', async () => {
    const { field } = open(null)
    await field('preview-toggle').trigger('click')
    expect(field('preview-toggle').attributes('aria-pressed')).toBe('true')
    expect(field('text-input').classes()).toContain('max-md:hidden')
  })

  it('cancels back to the page, or to the start for a new page', async () => {
    const edit = open(wikiPage())
    await edit.field('cancel').trigger('click')
    await flushPromises()
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'faq' } })
    const fresh = open(null)
    await fresh.field('cancel').trigger('click')
    await flushPromises()
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
  })

  it('offers only parents that leave a level', () => {
    const chain = Array.from({ length: 5 }, (_, i) => summary(`n${i}`, `Level ${i + 1}`, { parent_id: i ? `n${i - 1}` : null }))
    useWikiStore().pages = chain
    const { field } = open(null)
    const options = field('parent-input').findAll('option').map(o => o.text())
    expect(options).toContain('Level 4')
    expect(options).not.toContain('Level 5')
  })
})
