import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiEditor from './WikiEditor.vue'
import { ApiError } from '../lib/api'
import { pendingConfirm } from '../lib/confirm'
import { currentRoute, navigate } from '../lib/router'
import { wikiDirty } from '../lib/wikiNav'
import { loadWikiDraft, saveWikiDraft, wikiDraftKey } from '../lib/wikiDraft'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ createWikiPage: vi.fn(), changeWikiPage: vi.fn(), fetchWikiPage: vi.fn(), fetchWikiTree: vi.fn(), uploadWikiImage: vi.fn() }))
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
  sessionStorage.clear()
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

  it('keeps the unsaved mark and the text when it goes away with the text unsaved (the right to edit was lost)', async () => {
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('x')
    w.unmount()
    expect(wikiDirty.value).toBe(true)
    expect(loadWikiDraft(wikiDraftKey('', 'faq'))).toMatchObject({ content: 'x' })
  })

  it('lets go of the text once the member agreed to discard it', async () => {
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('x')
    wikiDirty.value = false // what the question "Discard your changes?" does on a yes
    w.unmount()
    expect(loadWikiDraft(wikiDraftKey('', 'faq'))).toBeNull()
  })

  it('does not pull the member back when a save that was in flight comes in after they left', async () => {
    let finish: (page: ReturnType<typeof wikiPage>) => void = () => {}
    api.changeWikiPage.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    void w.get('form').trigger('submit')
    await flushPromises()
    w.unmount()
    navigate('/wiki/other', { replace: true })
    useWikiStore().applyPage(wikiPage({ id: 'p-other', slug: 'other', title: 'Other' }))
    finish(wikiPage({ rev: 4, content: 'mine' }))
    await flushPromises()
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'other' } })
    expect(useWikiStore().page?.slug).toBe('other')
    expect(wikiDirty.value).toBe(false)
    expect(loadWikiDraft(wikiDraftKey('', 'faq'))).toBeNull()
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
    api.changeWikiPage.mockRejectedValueOnce(new ApiError(409, 'STALE_REVISION', STALE))
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
    api.changeWikiPage.mockRejectedValueOnce(new ApiError(409, 'STALE_REVISION', STALE))
    api.fetchWikiPage.mockResolvedValue(wikiPage({ rev: 4, content: 'theirs' }))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    await w.get('form').trigger('submit')
    await flushPromises()
    await field('conflict-theirs').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    expect(useWikiStore().page?.content).not.toBe('theirs') // nothing is dropped before the member agrees
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(useWikiStore().page?.content).toBe('theirs')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'faq' } })
  })

  it('keeps the text when the member declines to drop it for their version', async () => {
    api.changeWikiPage.mockRejectedValueOnce(new ApiError(409, 'STALE_REVISION', STALE))
    api.fetchWikiPage.mockResolvedValue(wikiPage({ rev: 4, content: 'theirs' }))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    await w.get('form').trigger('submit')
    await flushPromises()
    await field('conflict-theirs').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect((field('text-input').element as HTMLTextAreaElement).value).toBe('mine')
    expect(w.find('[data-testid="wiki-conflict"]').exists()).toBe(true)
    expect(wikiDirty.value).toBe(true)
  })

  it('shows an ordinary failure, also when the newer page cannot be read', async () => {
    api.changeWikiPage.mockRejectedValue(new ApiError(409, 'STALE_REVISION', STALE))
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

describe('a conflict is told by its code, not by its words', () => {
  it('shows a plain failure for another refusal that happens to use the same sentence', async () => {
    api.changeWikiPage.mockRejectedValueOnce(new ApiError(409, 'CONFLICT', STALE))
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('mine')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-testid="wiki-conflict"]').exists()).toBe(false)
    expect(field('error').text()).toContain('changed since you opened it')
    expect(api.fetchWikiPage).not.toHaveBeenCalled()
  })
})

describe('an unsaved draft', () => {
  const key = wikiDraftKey('', 'faq')

  it('is kept in the session while writing and gone once the editor is left', async () => {
    vi.useFakeTimers()
    try {
      const { w, field } = open(wikiPage())
      await field('text-input').setValue('half a thought')
      expect(loadWikiDraft(key)).toBeNull()
      vi.advanceTimersByTime(500)
      expect(loadWikiDraft(key)).toMatchObject({ content: 'half a thought', title: 'FAQ', baseRev: 3 })
      wikiDirty.value = false // the member agreed to discard it
      w.unmount()
      expect(loadWikiDraft(key)).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('comes back after a reload, says so, and saves on top of the revision it started from', async () => {
    saveWikiDraft(key, { title: 'FAQ', content: 'before the reload', summary: 'wip', slug: '', parent: '', baseRev: 2 })
    api.changeWikiPage.mockResolvedValue(wikiPage({ rev: 4 }))
    const { w, field } = open(wikiPage())
    expect((field('text-input').element as HTMLTextAreaElement).value).toBe('before the reload')
    expect(w.find('[data-testid="wiki-draft-restored"]').exists()).toBe(true)
    expect(wikiDirty.value).toBe(true)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.changeWikiPage).toHaveBeenCalledWith('faq', { base_rev: 2, title: 'FAQ', content: 'before the reload', summary: 'wip' })
    expect(loadWikiDraft(key)).toBeNull()
  })

  it('can be dropped for the text of the page', async () => {
    saveWikiDraft(key, { title: 'Other', content: 'before the reload', summary: '', slug: '', parent: '', baseRev: 3 })
    const { w, field } = open(wikiPage({ content: 'on the page' }))
    await field('draft-discard').trigger('click')
    expect((field('text-input').element as HTMLTextAreaElement).value).toBe('on the page')
    expect((field('title-input').element as HTMLInputElement).value).toBe('FAQ')
    expect(w.find('[data-testid="wiki-draft-restored"]').exists()).toBe(false)
    expect(wikiDirty.value).toBe(false)
    expect(loadWikiDraft(key)).toBeNull()
  })

  it('comes back for a page that was not created yet, and an empty one is no draft', () => {
    const fresh = wikiDraftKey('', null)
    saveWikiDraft(fresh, { title: '', content: '', summary: '', slug: '', parent: '', baseRev: 0 })
    expect(open(null).w.find('[data-testid="wiki-draft-restored"]').exists()).toBe(false)
    saveWikiDraft(fresh, { title: 'Raid', content: 'plan', summary: '', slug: 'raid', parent: 'g', baseRev: 0 })
    const { w, field } = open(null)
    expect(w.find('[data-testid="wiki-draft-restored"]').exists()).toBe(true)
    expect((field('title-input').element as HTMLInputElement).value).toBe('Raid')
    expect((field('slug-input').element as HTMLInputElement).value).toBe('raid')
  })

  it('is dropped again when the text goes back to what the page says before it was written down', async () => {
    vi.useFakeTimers()
    try {
      saveWikiDraft(key, { title: 'FAQ', content: 'older draft', summary: '', slug: '', parent: '', baseRev: 3 })
      const { field } = open(wikiPage())
      await field('text-input').setValue(wikiPage().content)
      vi.advanceTimersByTime(500)
      expect(loadWikiDraft(key)).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('is ignored when it says what the page already says', () => {
    saveWikiDraft(key, { title: 'FAQ', content: wikiPage().content, summary: '', slug: '', parent: '', baseRev: 1 })
    const { w } = open(wikiPage())
    expect(w.find('[data-testid="wiki-draft-restored"]').exists()).toBe(false)
  })

  it('asks the browser before the tab closes, only while there is something unsaved', async () => {
    const { field } = open(wikiPage())
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    await field('text-input').setValue('changed')
    const dirty = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirty)
    expect(dirty.defaultPrevented).toBe(true)
    expect(loadWikiDraft(key)).toMatchObject({ content: 'changed' })
  })

  it('stops asking once the editor is gone', async () => {
    const { w, field } = open(wikiPage())
    await field('text-input').setValue('changed')
    w.unmount()
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
  })
})

describe('pictures', () => {
  const URL = '/api/media/0b7e1c52-9a43-4f5e-8c0d-3d1f2a9b6e77'
  const png = (name = 'Site map.png') => new File(['x'], name, { type: 'image/png' })

  it('uploads a picked file and puts its Markdown where the cursor is', async () => {
    api.uploadWikiImage.mockResolvedValue({ id: 'i', url: URL, markdown: `![](${URL})`, size: 1 })
    const { w, field } = open(wikiPage({ content: 'before after' }))
    const area = field('text-input').element as HTMLTextAreaElement
    area.setSelectionRange(7, 7)
    const input = field('image-input').element as HTMLInputElement
    Object.defineProperty(input, 'files', { value: [png()], configurable: true })
    await field('image-input').trigger('change')
    await flushPromises()
    expect(api.uploadWikiImage).toHaveBeenCalledTimes(1)
    expect(area.value).toBe(`before ![Site map](${URL})after`)
    expect(w.find('[data-testid="wiki-uploading"]').exists()).toBe(false)
  })

  it('opens the file picker from the toolbar', async () => {
    const { field } = open(null)
    const click = vi.spyOn(field('image-input').element as HTMLInputElement, 'click')
    await field('image-button').trigger('click')
    expect(click).toHaveBeenCalled()
  })

  it('takes a pasted or dropped picture instead of the default, and leaves pasted text alone', async () => {
    api.uploadWikiImage.mockResolvedValue({ id: 'i', url: URL, markdown: '', size: 1 })
    const { field } = open(null)
    const paste = new Event('paste', { cancelable: true }) as ClipboardEvent
    Object.defineProperty(paste, 'clipboardData', { value: { files: [png('shot.png')] } })
    field('text-input').element.dispatchEvent(paste)
    expect(paste.defaultPrevented).toBe(true)
    const drop = new Event('drop', { cancelable: true }) as DragEvent
    Object.defineProperty(drop, 'dataTransfer', { value: { files: [png('b.png')] } })
    field('text-input').element.dispatchEvent(drop)
    expect(drop.defaultPrevented).toBe(true)
    await flushPromises()
    expect(api.uploadWikiImage).toHaveBeenCalledTimes(2)
    const text = new Event('paste', { cancelable: true }) as ClipboardEvent
    Object.defineProperty(text, 'clipboardData', { value: { files: [] } })
    field('text-input').element.dispatchEvent(text)
    expect(text.defaultPrevented).toBe(false)
  })

  it('cannot be saved while a picture is on its way', async () => {
    let done!: (v: unknown) => void
    api.uploadWikiImage.mockReturnValue(new Promise(r => { done = r }))
    const { w, field } = open(null)
    await field('title-input').setValue('Maps')
    const input = field('image-input').element as HTMLInputElement
    Object.defineProperty(input, 'files', { value: [png()], configurable: true })
    await field('image-input').trigger('change')
    expect(w.find('[data-testid="wiki-uploading"]').exists()).toBe(true)
    expect(field('save').attributes('disabled')).toBeDefined()
    done({ id: 'i', url: URL, markdown: '', size: 1 })
    await flushPromises()
    expect(field('save').attributes('disabled')).toBeUndefined()
  })

  it('says why a picture was not added', async () => {
    const { field } = open(null)
    const input = field('image-input').element as HTMLInputElement
    Object.defineProperty(input, 'files', { value: [new File(['x'], 'a.svg', { type: 'image/svg+xml' })], configurable: true })
    await field('image-input').trigger('change')
    expect(api.uploadWikiImage).not.toHaveBeenCalled()
    api.uploadWikiImage.mockRejectedValue(new ApiError(409, 'CONFLICT', 'the wiki holds at most 200 pictures'))
    Object.defineProperty(input, 'files', { value: [png()], configurable: true })
    await field('image-input').trigger('change')
    await flushPromises()
    const toasts = useToastStore().toasts
    expect(toasts).toHaveLength(2)
    expect(toasts.every(t => t.type === 'error')).toBe(true)
    expect((field('text-input').element as HTMLTextAreaElement).value).toBe('')
  })
})
