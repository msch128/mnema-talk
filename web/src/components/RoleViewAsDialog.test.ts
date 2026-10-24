import type { ApiOptions } from '../lib/api'
import type { ChatViewAsRole } from '../types/rest'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RoleViewAsDialog from './RoleViewAsDialog.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const id = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`
const ROLE = id(1)
const channel = (n: number, name: string, type: 'text' | 'voice', canJoin: boolean, permissions: string[]) =>
  ({ id: id(n), number: n, name, type, can_join: canJoin, permissions })

const preview: ChatViewAsRole = {
  role_id: ROLE, role_name: 'Guest', administrator: false,
  uncategorized: [channel(10, 'welcome', 'text', false, ['view_channel'])],
  categories: [{ id: id(5), name: 'Community', channels: [
    channel(11, 'general', 'text', false, ['view_channel', 'send_messages']),
    channel(12, 'Lounge', 'voice', true, ['view_channel', 'connect', 'speak']),
    channel(13, 'Quiet room', 'voice', false, ['view_channel'])
  ] }]
}

async function open(data: ChatViewAsRole = preview) {
  apiMock.mockResolvedValue(data)
  const w = mount(RoleViewAsDialog, { props: { roleId: ROLE, roleName: 'Guest' }, attachTo: document.body })
  await flushPromises()
  return w
}

beforeEach(() => {
  setLocale('en')
  apiMock.mockReset()
  document.body.replaceChildren()
})

describe('RoleViewAsDialog', () => {
  it('asks the server for the role and shows only the channels it returned', async () => {
    const w = await open()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/roles/${ROLE}/view-as`, { decode: expect.any(Function) })
    const names = w.findAll('[data-testid="view-as-sidebar"] [data-channel]').map(b => b.attributes('data-channel'))
    expect(names).toEqual(['welcome', 'general', 'Lounge', 'Quiet room'])
    expect(w.get('[data-testid="view-as-category"]').text()).toBe('Community')
    w.unmount()
  })

  it('marks Talks the role can and cannot join, and text channels not at all', async () => {
    const w = await open()
    const joins = w.findAll('[data-testid="view-as-join"]').map(j => j.text())
    expect(joins).toEqual(['can join', 'can’t join'])
    w.unmount()
  })

  it('lists the effective permissions of the selected channel', async () => {
    const w = await open()
    const allowed = () => w.findAll('[data-permission]').filter(li => li.text().includes('allowed') && !li.text().includes('not allowed')).map(li => li.attributes('data-permission'))
    expect(allowed()).toEqual(['view_channel'])
    await w.get('[data-channel="Lounge"]').trigger('click')
    expect(allowed()).toEqual(['view_channel', 'connect', 'speak'])
    expect(w.get('[data-testid="view-as-permissions"]').text()).toContain('Permissions in Lounge')
    w.unmount()
  })

  it('says so when the role has Administrator or sees nothing', async () => {
    const admin = await open({ ...preview, administrator: true })
    expect(admin.find('[data-testid="view-as-admin"]').exists()).toBe(true)
    admin.unmount()
    const none = await open({ ...preview, uncategorized: [], categories: [] })
    expect(none.find('[data-testid="view-as-empty"]').exists()).toBe(true)
    none.unmount()
  })

  it('shows a failure and leaves through the exit button', async () => {
    apiMock.mockRejectedValue(new Error('down'))
    const w = mount(RoleViewAsDialog, { props: { roleId: ROLE, roleName: 'Guest' }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    await w.get('[data-testid="view-as-exit"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
})
