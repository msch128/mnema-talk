import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiMove from './WikiMove.vue'
import { ApiError } from '../lib/api'
import { currentRoute, navigate } from '../lib/router'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ changeWikiPage: vi.fn(), fetchWikiTree: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  Object.values(api).forEach(fn => fn.mockReset())
  api.fetchWikiTree.mockResolvedValue([])
  grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
  navigate('/wiki/faq', { replace: true })
  useWikiStore().pages = [summary('top', 'Guides'), summary('p1', 'FAQ', { parent_id: 'top' }), summary('c1', 'Child', { parent_id: 'p1' }), summary('x', 'Other')]
})

const field = (w: ReturnType<typeof mount>, id: string) => w.get(`[data-testid="wiki-move-${id}"]`)

describe('WikiMove', () => {
  it('offers neither the page itself nor its subpages as a parent', () => {
    const w = mount(WikiMove, { props: { page: wikiPage({ parent_id: 'top' }) } })
    const options = field(w, 'parent').findAll('option').map(o => o.text())
    expect(options).toContain('Guides')
    expect(options).toContain('Other')
    expect(options).not.toContain('FAQ')
    expect(options).not.toContain('Child')
  })

  it('keeps a subtree that would pass five levels out of the choices', () => {
    useWikiStore().pages = [
      summary('a', 'L1'), summary('b', 'L2', { parent_id: 'a' }), summary('c', 'L3', { parent_id: 'b' }), summary('d', 'L4', { parent_id: 'c' }),
      summary('m', 'Mover'), summary('m2', 'Mover child', { parent_id: 'm' })
    ]
    const w = mount(WikiMove, { props: { page: wikiPage({ id: 'm', slug: 'mover', parent_id: null }) } })
    const options = field(w, 'parent').findAll('option').map(o => o.text())
    expect(options).toContain('L3') // the mover has two levels, so it fits below L3
    expect(options).not.toContain('L4')
  })

  it('sends only what changed', async () => {
    api.changeWikiPage.mockResolvedValue(wikiPage({ slug: 'help', parent_id: null }))
    const w = mount(WikiMove, { props: { page: wikiPage({ parent_id: 'top', position: 1 }) } })
    await field(w, 'slug').setValue('help')
    await field(w, 'parent').setValue('')
    await field(w, 'position').setValue(4)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.changeWikiPage).toHaveBeenCalledWith('faq', { base_rev: 3, slug: 'help', parent_id: '00000000-0000-0000-0000-000000000000', position: 4 })
    expect(w.emitted('done')).toHaveLength(1)
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'help' } })
  })

  it('does not turn an emptied position field into position 0', async () => {
    api.changeWikiPage.mockResolvedValue(wikiPage({ slug: 'help' }))
    const w = mount(WikiMove, { props: { page: wikiPage({ position: 7 }) } })
    await field(w, 'slug').setValue('help')
    await field(w, 'position').setValue('')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.changeWikiPage).toHaveBeenCalledWith('faq', { base_rev: 3, slug: 'help' })
  })

  it('moves below another page and stays on the address when it did not change', async () => {
    api.changeWikiPage.mockResolvedValue(wikiPage({ parent_id: 'x' }))
    const w = mount(WikiMove, { props: { page: wikiPage() } })
    await field(w, 'parent').setValue('x')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.changeWikiPage).toHaveBeenCalledWith('faq', { base_rev: 3, parent_id: 'x' })
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'faq' } })
  })

  it('shows the refusal and stays open', async () => {
    api.changeWikiPage.mockRejectedValue(new ApiError(409, 'CONFLICT', 'that address is already used by a page or a redirect'))
    const w = mount(WikiMove, { props: { page: wikiPage() } })
    await field(w, 'slug').setValue('taken')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(field(w, 'error').text()).toContain('already used')
    expect(w.emitted('done')).toBeUndefined()
  })

  it('cancels', async () => {
    const w = mount(WikiMove, { props: { page: wikiPage() } })
    await w.get('button[type="button"]').trigger('click')
    expect(w.emitted('done')).toHaveLength(1)
  })
})
