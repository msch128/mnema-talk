import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import { ApiError } from '../../shared/lib/api'
import ComponentSelect from './ComponentSelect.vue'
import MessageComponents from './MessageComponents.vue'
import { useChatStore } from '../../stores/chat'
import { usePermissionsStore } from '../../stores/permissions'
import { setLocale } from '../../i18n'
import { categoryFixture, channelFixture, fixtureId, userFixture } from '../../test-fixtures.fixture'
import type { ChatComponent } from '../../types/rest'
import type { Role } from '../../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const env: ChatComponent = {
  type: 3, custom_id: 'env', placeholder: 'Environment',
  options: [{ label: 'Staging', value: 'staging', description: 'The test system' }, { label: 'Production', value: 'prod', default: true }, { label: 'Local', value: 'local' }]
}
const role = (n: number, extra: Partial<Role> = {}): Role => ({ id: fixtureId(70 + n), name: `Role${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...extra })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset().mockResolvedValue(undefined)
  sendMock.mockReset().mockResolvedValue(true)
})

const sendMock = vi.fn<(values: string[]) => Promise<boolean>>()
const mountSelect = (component: ChatComponent) => mount(ComponentSelect, { props: { component, send: sendMock }, attachTo: document.body })
const options = (w: ReturnType<typeof mountSelect>) => w.findAll('[role="option"]')
const key = async (w: ReturnType<typeof mountSelect>, k: string) => w.get('[data-testid="component-select-panel"]').trigger('keydown', { key: k })

describe('a string select', () => {
  it('shows its default, opens a listbox and sends one pick with Enter', async () => {
    const w = mountSelect(env)
    const trigger = w.get('button[aria-haspopup="listbox"]')
    expect(trigger.text()).toContain('Production')
    await trigger.trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w.get('[role="listbox"]').element)
    expect(options(w).map(o => o.attributes('aria-selected'))).toEqual(['false', 'true', 'false'])
    expect(w.text()).toContain('The test system')
    await key(w, 'ArrowDown')
    await key(w, 'ArrowDown')
    expect(w.get('[role="listbox"]').attributes('aria-activedescendant')).toBe(options(w)[2]!.attributes('id'))
    await key(w, 'Enter')
    await flushPromises()
    expect(sendMock.mock.calls).toEqual([[['local']]])
    expect(w.find('[role="listbox"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(trigger.text()).toContain('Local')
    w.unmount()
  })

  it('jumps with Home and End, and closes when focus leaves it', async () => {
    const outside = document.createElement('button')
    document.body.appendChild(outside)
    const w = mountSelect(env)
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    await key(w, 'End')
    expect(w.get('[role="listbox"]').attributes('aria-activedescendant')).toBe(options(w)[2]!.attributes('id'))
    await key(w, 'Home')
    expect(w.get('[role="listbox"]').attributes('aria-activedescendant')).toBe(options(w)[0]!.attributes('id'))
    await key(w, 'ArrowUp')
    expect(w.get('[role="listbox"]').attributes('aria-activedescendant')).toBe(options(w)[2]!.attributes('id'))
    await w.get('[role="listbox"]').trigger('focusout', { relatedTarget: outside })
    expect(w.find('[role="listbox"]').exists()).toBe(false)
    expect(sendMock).not.toHaveBeenCalled()
    w.unmount()
    outside.remove()
  })

  it('stays shut while disabled or while its pick is on the way', async () => {
    const w = mount(ComponentSelect, { props: { component: env, pending: true, send: sendMock } })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    expect(w.find('[role="listbox"]').exists()).toBe(false)
    expect(w.get('button[aria-haspopup="listbox"]').attributes('aria-busy')).toBe('true')
  })

  it('closes with Escape and sends nothing', async () => {
    const w = mountSelect(env)
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    await key(w, 'Escape')
    expect(w.find('[role="listbox"]').exists()).toBe(false)
    expect(sendMock).not.toHaveBeenCalled()
    w.unmount()
  })

  it('takes several values only within its bounds and sends them with Confirm', async () => {
    const w = mountSelect({ ...env, options: env.options!.map(o => ({ ...o, default: false })), min_values: 2, max_values: 2 })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    expect(w.get('[role="listbox"]').attributes('aria-multiselectable')).toBe('true')
    expect(w.get('[data-testid="component-select-bounds"]').text()).toBe('Pick 2')
    const confirm = () => w.get('[data-testid="component-select-confirm"]')
    await options(w)[0]!.trigger('click')
    expect(confirm().attributes('disabled')).toBeDefined()
    await key(w, 'ArrowDown')
    await key(w, ' ')
    await options(w)[2]!.trigger('click')
    expect(options(w)[2]!.attributes('aria-selected')).toBe('false')
    await confirm().trigger('click')
    expect(sendMock.mock.calls).toEqual([[['staging', 'prod']]])
    w.unmount()
  })

  it('names a range when the bounds differ', async () => {
    const w = mountSelect({ ...env, min_values: 0, max_values: 3 })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    expect(w.get('[data-testid="component-select-bounds"]').text()).toBe('Pick 0 to 3')
    w.unmount()
  })
})

describe('user, role and channel selects', () => {
  it('search the members this member sees', async () => {
    useChatStore().members = [userFixture({ id: fixtureId(1), username: 'max', display_name: 'Max' }), userFixture({ id: fixtureId(2), username: 'mia' })]
    const w = mountSelect({ type: 5, custom_id: 'who' })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    await flushPromises()
    const search = w.get('[data-testid="component-select-search"]')
    expect(document.activeElement).toBe(search.element)
    expect(options(w).map(o => o.text())).toEqual(['Max@max', 'mia@mia'])
    await search.setValue('zzz')
    expect(w.text()).toContain('Nothing matches.')
    await search.trigger('keydown', { key: 'ArrowDown' })
    await search.trigger('keydown', { key: 'Enter' })
    expect(sendMock).not.toHaveBeenCalled()
    await search.setValue('mi')
    expect(options(w).map(o => o.attributes('data-value'))).toEqual([fixtureId(2)])
    await search.trigger('keydown', { key: 'Enter' })
    expect(sendMock.mock.calls).toEqual([[[fixtureId(2)]]])
    w.unmount()
  })

  it('offer roles without @everyone and the managed bot roles', async () => {
    usePermissionsStore().roles = [role(1, { is_everyone: true, name: '@everyone' }), role(2, { managed: true, name: 'Bot' }), role(3, { name: 'Helpers', color: 0x2da771 })]
    const w = mountSelect({ type: 6, custom_id: 'role' })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    expect(options(w).map(o => o.text())).toEqual(['Helpers'])
    w.unmount()
  })

  it('offer text and forum channels, never a Talk', async () => {
    const chat = useChatStore()
    chat.categories = [categoryFixture({ channels: [channelFixture({ id: fixtureId(5), name: 'general' }), channelFixture({ id: fixtureId(6), name: 'lounge', type: 'voice' })] })]
    const w = mountSelect({ type: 8, custom_id: 'where' })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    expect(options(w).map(o => o.text())).toEqual(['#general'])
    w.unmount()
  })

  it('send what was picked to the server, through the message', async () => {
    useChatStore().members = [userFixture({ id: fixtureId(1), username: 'max' })]
    const w = mount(MessageComponents, { props: { components: [{ type: 1, components: [{ type: 5, custom_id: 'who' }] }], endpoint: '/x', channelId: 'c', messageId: 'm', botName: 'B' } })
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    await w.get('[role="option"]').trigger('click')
    expect(apiMock).toHaveBeenCalledWith('/x', expect.objectContaining({ json: { custom_id: 'who', values: [fixtureId(1)] } }))
  })

  it('go back to the earlier choice when the server refuses the pick, and say why', async () => {
    apiMock.mockRejectedValue(new ApiError(409, 'conflict', 'not connected'))
    const w = mount(MessageComponents, { props: { components: [{ type: 1, components: [env] }], endpoint: '/x', channelId: 'c', messageId: 'm', botName: 'Deployer' } })
    const trigger = () => w.get('button[aria-haspopup="listbox"]')
    await trigger().trigger('click')
    await w.findAll('[role="option"]')[0]!.trigger('click')
    await flushPromises()
    expect(trigger().text()).toContain('Production')
    expect(trigger().text()).not.toContain('Staging')
    expect(w.get('[data-testid="component-error"]').text()).toBe('Deployer is not connected right now. Try again later.')
  })
})
