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

  it('goes back past every wiki page that was opened since the wiki was', () => {
    navigate('/', { replace: true })
    navigate('/c/general')
    openWiki()
    openWiki('rules')
    openWiki('faq')
    const go = vi.spyOn(window.history, 'go').mockImplementation(() => {})
    closeWiki()
    expect(go).toHaveBeenCalledWith(-3)
  })

  it('goes back one entry when the wiki was opened by the entry before it', () => {
    navigate('/c/general')
    openWiki()
    const go = vi.spyOn(window.history, 'go').mockImplementation(() => {})
    closeWiki()
    expect(go).toHaveBeenCalledWith(-1)
  })

  it('does not walk out of the app when the wiki was the first thing opened', () => {
    window.history.replaceState({ idx: 0 }, '', '/wiki/faq')
    currentRoute.value = parseRoute('/wiki/faq')
    openWiki('rules')
    const go = vi.spyOn(window.history, 'go').mockImplementation(() => {})
    closeWiki()
    expect(go).not.toHaveBeenCalled()
    expect(currentRoute.value.view).toBe('root')
  })
})

describe('Back and Forward with an unsaved draft', () => {
  function pop(idx: number, path: string) {
    window.history.replaceState({ idx }, '', path)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }

  it('put the browser back, ask, and move on only when the member agrees', async () => {
    openWiki()
    openWiki('faq')
    wikiDirty.value = true
    const go = vi.spyOn(window.history, 'go').mockImplementation(() => {})
    const here = window.location.pathname
    // The browser went Back one entry.
    const at = window.history.state.idx
    pop(at - 1, '/wiki')
    expect(go).toHaveBeenCalledWith(1)
    pop(at, here) // the browser reports the step back to where it was
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'faq' } })
    expect(pendingConfirm.value).not.toBeNull()
    pendingConfirm.value?.resolve(true)
    await vi.waitFor(() => expect(go).toHaveBeenCalledWith(-1))
    expect(wikiDirty.value).toBe(false)
    pop(at - 1, '/wiki') // and the second step, now allowed
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
  })

  it('stay where they are when the member keeps editing', async () => {
    openWiki()
    openWiki('faq')
    wikiDirty.value = true
    const go = vi.spyOn(window.history, 'go').mockImplementation(() => {})
    const at = window.history.state.idx
    const here = window.location.pathname
    pop(at - 1, '/wiki')
    pop(at, here)
    pendingConfirm.value?.resolve(false)
    await Promise.resolve()
    await Promise.resolve()
    expect(go).toHaveBeenCalledTimes(1)
    expect(wikiDirty.value).toBe(true)
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'faq' } })
  })

  it('follow the browser at once when nothing is unsaved', () => {
    openWiki()
    openWiki('faq')
    pop(window.history.state.idx - 1, '/wiki')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
  })
})
