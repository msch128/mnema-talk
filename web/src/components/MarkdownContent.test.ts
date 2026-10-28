import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import MarkdownContent from './MarkdownContent.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { userFixture, fixtureId } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)
beforeEach(() => {
  setActivePinia(createPinia())
  vi.spyOn(useChatStore(), 'openUserProfile').mockResolvedValue(undefined)
  useChatStore().members = [userFixture({ username: 'Member', id: fixtureId(20) }), userFixture({ username: '', id: fixtureId(21) })]
})

describe('MarkdownContent interaction delegation', () => {
  it('opens known member mentions by mouse and keyboard while ignoring group mentions', async () => {
    useAuthStore().user = userFixture({ username: 'Member' })
    const w = mount(MarkdownContent, { props: { content: '@Member @all @here' } })
    const mentions = w.findAll('[data-mention]')
    expect(mentions).toHaveLength(3)
    await w.find('[data-mention="member"]').trigger('click')
    expect(useChatStore().openUserProfile).toHaveBeenCalledWith(userFixture({ username: 'Member', id: fixtureId(20) }))
    vi.mocked(useChatStore().openUserProfile).mockClear()
    await w.find('[data-mention="member"]').trigger('keydown', { key: 'Enter' })
    expect(useChatStore().openUserProfile).toHaveBeenCalledWith(userFixture({ username: 'Member', id: fixtureId(20) }))
    vi.mocked(useChatStore().openUserProfile).mockClear()
    await w.find('[data-mention="all"]').trigger('click')
    await w.find('[data-mention="here"]').trigger('keydown', { key: 'Enter' })
    expect(useChatStore().openUserProfile).not.toHaveBeenCalled()
  })

  it('ignores missing and stale mention targets, including nested clicks', async () => {
    const w = mount(MarkdownContent)
    const body = w.find('.markdown-body')
    const empty = document.createElement('span')
    empty.className = 'md-mention'
    empty.dataset.mention = ''
    body.element.append(empty)
    empty.click()
    const stale = document.createElement('span')
    stale.className = 'md-mention'
    stale.dataset.mention = 'missing'
    const child = document.createElement('strong')
    stale.append(child)
    body.element.append(stale)
    child.click()
    expect(useChatStore().openUserProfile).not.toHaveBeenCalled()
    await body.trigger('keydown', { key: 'Enter' })
    await body.trigger('keydown', { key: 'ArrowDown' })
    await body.trigger('click')
    expect(useChatStore().openUserProfile).not.toHaveBeenCalled()
  })

  it('hides a spoiler from assistive technology until it is revealed', async () => {
    const w = mount(MarkdownContent, { props: { content: '||secret||' } })
    const spoiler = w.find('.md-spoiler')
    expect(spoiler.attributes('aria-label')).toBe('Spoiler')
    expect(spoiler.attributes('aria-expanded')).toBe('false')
    await spoiler.trigger('click')
    expect(spoiler.attributes('aria-label')).toBeUndefined()
    expect(spoiler.attributes('aria-expanded')).toBe('true')
    await spoiler.trigger('keydown', { key: 'Enter' })
    expect(spoiler.attributes('aria-label')).toBe('Spoiler')
    expect(spoiler.attributes('aria-expanded')).toBe('false')
  })

  it('reveals and conceals spoilers with nested clicks, Enter and Space', async () => {
    const w = mount(MarkdownContent, { props: { content: '||secret **nested**||' } })
    const spoiler = w.find('.md-spoiler')
    await spoiler.find('strong').trigger('click')
    expect(spoiler.classes()).toContain('revealed')
    await spoiler.trigger('click')
    expect(spoiler.classes()).not.toContain('revealed')
    await spoiler.trigger('keydown', { key: 'Enter' })
    expect(spoiler.classes()).toContain('revealed')
    await spoiler.trigger('keydown', { key: ' ' })
    expect(spoiler.classes()).not.toContain('revealed')
    await spoiler.trigger('keydown', { key: 'Tab' })
    expect(spoiler.classes()).not.toContain('revealed')
  })

  it('handles event targets outside the Element hierarchy', () => {
    const w = mount(MarkdownContent)
    const target = document.createTextNode('plain')
    w.find('.markdown-body').element.append(target)
    target.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    target.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Enter' }))
    target.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: ' ' }))
    expect(useChatStore().openUserProfile).not.toHaveBeenCalled()
  })

  it('renders preview cards only for plain URLs and caps them at three', () => {
    const w = mount(MarkdownContent, { props: { content: 'https://example.org/one https://example.org/two https://example.org/three https://example.org/four `https://example.org/code` ||https://example.org/spoiler||' }, global: { stubs: { LinkPreviewCard: { props: ['url'], template: '<a class="preview">{{ url }}</a>' } } } })
    expect(w.findAll('.preview').map(link => link.text())).toEqual(['https://example.org/one', 'https://example.org/two', 'https://example.org/three'])
  })
})

describe('MarkdownContent role pills', () => {
  const role = (id: string, name: string) => ({
    id, name, color: 0, position: 1, permissions: 0, hoist: false, mentionable: true, is_everyone: false, max_upload_mb: null
  })

  it("draws only the roles the message recorded, and marks the reader's own", () => {
    const permissions = usePermissionsStore()
    permissions.roles = [role('r1', 'Dev Team'), role('r2', 'Ops')]
    permissions.info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: ['r1'], top_position: 1, catalog: [], channels: [] }
    const w = mount(MarkdownContent, { props: { content: '@Dev Team and @Ops and @gone', mentionRoles: ['r1', 'missing'] } })
    const pills = w.findAll('.md-mention-role')
    expect(pills.map(p => p.text())).toEqual(['@Dev Team'])
    expect(pills[0]?.classes()).toContain('md-mention-me')
  })
})
