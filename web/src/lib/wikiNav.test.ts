import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { pendingConfirm } from './confirm'
import { currentRoute, navigate, parseRoute } from './router'
import { closeWiki, goWiki, mayLeaveWiki, openWiki, wikiDirty } from './wikiNav'

beforeEach(() => {
  wikiDirty.value = false
  navigate('/', { replace: true })
})
afterEach(() => {
  pendingConfirm.value?.resolve(false)
  vi.restoreAllMocks()
})

describe('wiki navigation', () => {
  it('lets the member go when nothing is unsaved', async () => {
    expect(await mayLeaveWiki()).toBe(true)
    await goWiki({ mode: 'page', slug: 'faq' })
    expect(currentRoute.value).toMatchObject({ view: 'wiki', wiki: { mode: 'page', slug: 'faq' } })
  })

  it('asks before unsaved text is thrown away, and stays when the member says no', async () => {
    navigate('/wiki/faq/edit')
    wikiDirty.value = true
    const going = goWiki({ mode: 'home' })
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(false)
    await going
    expect(currentRoute.value).toMatchObject({ view: 'wiki', wiki: { mode: 'edit', slug: 'faq' } })
    expect(wikiDirty.value).toBe(true)
  })

  it('leaves and forgets the unsaved text when the member agrees', async () => {
    navigate('/wiki/faq/edit')
    wikiDirty.value = true
    const going = goWiki({ mode: 'home' })
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(true)
    await going
    expect(wikiDirty.value).toBe(false)
    expect(currentRoute.value).toMatchObject({ view: 'wiki', wiki: { mode: 'home' } })
  })

  it('opens the start page or a page from anywhere', () => {
    openWiki()
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
    openWiki('rules')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'rules' } })
  })

  it('closes to the start of the app when there is nothing to go back to', () => {
    window.history.replaceState({ idx: 0 }, '', '/wiki')
    currentRoute.value = parseRoute('/wiki')
    wikiDirty.value = true
    closeWiki()
    expect(wikiDirty.value).toBe(false)
    expect(currentRoute.value.view).not.toBe('wiki')
  })

  it('goes back in the history when the app has one', () => {
    navigate('/')
    navigate('/wiki')
    const back = vi.spyOn(window.history, 'back').mockImplementation(() => {})
    closeWiki()
    expect(back).toHaveBeenCalled()
  })
})
