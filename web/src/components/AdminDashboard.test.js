import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import AdminDashboard from './AdminDashboard.vue'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import { setLocale } from '../i18n'

const mockUsers = [
  {
    id: 'user-admin-1',
    username: 'herzog',
    display_name: 'Herzog',
    role: 'admin',
    disabled: false,
    last_seen_at: '2026-10-01T12:00:00Z',
    avatar_url: ''
  },
  {
    id: 'user-member-2',
    username: 'alice',
    display_name: 'Alice Wonder',
    role: 'member',
    disabled: false,
    last_seen_at: '2026-10-02T15:30:00Z',
    avatar_url: ''
  },
  {
    id: 'user-member-3',
    username: 'bob',
    display_name: 'Bob Builder',
    role: 'member',
    disabled: true,
    last_seen_at: null,
    avatar_url: ''
  }
]

const mockChannelsData = {
  categories: [
    {
      id: 'cat-1',
      name: 'Text Channels',
      sort_order: 0,
      channels: [
        { id: 'ch-1', name: 'general', type: 'text', topic: 'Chatting', sort_order: 0, category_id: 'cat-1' },
        { id: 'ch-2', name: 'games', type: 'text', topic: 'Gaming', sort_order: 1, category_id: 'cat-1' }
      ]
    },
    {
      id: 'cat-2',
      name: 'Voice Channels',
      sort_order: 1,
      channels: [
        { id: 'ch-voice-1', name: 'Lounge', type: 'voice', topic: '', sort_order: 0, category_id: 'cat-2' }
      ]
    }
  ],
  uncategorized: [
    { id: 'ch-uncat-1', name: 'welcome', type: 'text', topic: 'Welcome', sort_order: 0, category_id: null }
  ]
}

const mockStats = { total_files: 5, total_size_bytes: 1048576, deleted_files: 1 }
const mockMedia = [
  { id: 'm-1', original_filename: 'pic.png', size_bytes: 512, mime_type: 'image/png', url: 'http://example.com/pic.png', is_deleted: false, uploader_name: 'Alice' }
]
const mockInvites = [
  { id: 'inv-1', code: 'TEST1234', uses_count: 0, max_uses: 5, expires_at: null }
]

const apiMock = vi.fn((url, options) => {
  if (url === '/api/admin/users') return Promise.resolve(JSON.parse(JSON.stringify(mockUsers)))
  if (url === '/api/channels') return Promise.resolve(JSON.parse(JSON.stringify(mockChannelsData)))
  if (url === '/api/admin/media/stats') return Promise.resolve(mockStats)
  if (url.startsWith('/api/admin/media?')) return Promise.resolve(mockMedia)
  if (url === '/api/admin/invites') return Promise.resolve(mockInvites)
  if (options?.method === 'PATCH' || options?.method === 'POST' || options?.method === 'PUT' || options?.method === 'DELETE') {
    return Promise.resolve({ ok: true })
  }
  return Promise.resolve(null)
})

vi.mock('../lib/api', () => ({
  api: (...args) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

const confirmMock = vi.fn(() => Promise.resolve(true))
vi.mock('../lib/confirm', () => ({
  confirm: (...args) => confirmMock(...args)
}))

let wrapper

async function createWrapper() {
  const auth = useAuthStore()
  auth.user = { id: 'user-admin-1', username: 'herzog', display_name: 'Herzog', role: 'admin' }
  wrapper = mount(AdminDashboard, {
    attachTo: document.body
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockClear()
  confirmMock.mockClear()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

describe('AdminDashboard - Tabs Navigation', () => {
  it('renders all four navigation tabs with default to users tab', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    expect(w.find('[data-testid="tab-users"]').exists()).toBe(true)
    expect(w.find('[data-testid="tab-channels"]').exists()).toBe(true)
    expect(w.find('[data-testid="tab-invites"]').exists()).toBe(true)
    expect(w.find('[data-testid="tab-media"]').exists()).toBe(true)

    expect(w.find('[data-testid="tab-users"]').attributes('aria-pressed')).toBe('true')
  })

  it('can switch between tabs', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    await w.find('[data-testid="tab-channels"]').trigger('click')
    expect(w.find('[data-testid="tab-channels"]').attributes('aria-pressed')).toBe('true')
    expect(w.find('[data-testid="save-layout-button"]').exists()).toBe(true)

    await w.find('[data-testid="tab-invites"]').trigger('click')
    expect(w.find('[data-testid="tab-invites"]').attributes('aria-pressed')).toBe('true')

    await w.find('[data-testid="tab-media"]').trigger('click')
    expect(w.find('[data-testid="tab-media"]').attributes('aria-pressed')).toBe('true')
  })
})

describe('AdminDashboard - Users Tab', () => {
  it('fetches and displays the user table with correct attributes', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const rows = w.findAll('[data-testid="user-row"]')
    expect(rows).toHaveLength(3)

    // Admin user row
    const adminRow = rows[0]
    expect(adminRow.find('[data-testid="user-username"]').text()).toContain('@herzog')
    expect(adminRow.find('[data-testid="user-displayname"]').text()).toBe('Herzog')
    expect(adminRow.find('[data-testid="user-role"]').text()).toBe('Admin')
    expect(adminRow.find('[data-testid="user-status"]').text()).toBe('Active')

    // Disabled user row
    const bobRow = rows[2]
    expect(bobRow.find('[data-testid="user-username"]').text()).toContain('@bob')
    expect(bobRow.find('[data-testid="user-displayname"]').text()).toBe('Bob Builder')
    expect(bobRow.find('[data-testid="user-status"]').text()).toBe('Disabled')
    expect(bobRow.find('[data-testid="user-lastseen"]').text()).toBe('Never')
  })

  it('filters users by search query', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const searchInput = w.find('input[type="search"]')
    await searchInput.setValue('Alice')
    await nextTick()

    const rows = w.findAll('[data-testid="user-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].find('[data-testid="user-displayname"]').text()).toBe('Alice Wonder')
  })

  it('disables user actions on admin accounts', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const adminRow = w.findAll('[data-testid="user-row"]')[0]
    expect(adminRow.find('[data-testid="action-disable"]').exists()).toBe(false)
    expect(adminRow.find('[data-testid="action-reset-password"]').exists()).toBe(false)
    expect(adminRow.find('[data-testid="action-revoke-sessions"]').exists()).toBe(false)
  })

  it('handles disabling a member with confirmation dialog', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const memberRow = w.findAll('[data-testid="user-row"]')[1] // Alice
    const disableBtn = memberRow.find('[data-testid="action-disable"]')
    expect(disableBtn.exists()).toBe(true)

    await disableBtn.trigger('click')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/user-member-2/disable', { method: 'POST' })
  })

  it('handles enabling a previously disabled member without extra prompt', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const bobRow = w.findAll('[data-testid="user-row"]')[2] // Bob
    const enableBtn = bobRow.find('[data-testid="action-disable"]')
    expect(enableBtn.exists()).toBe(true)

    await enableBtn.trigger('click')
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/user-member-3/enable', { method: 'POST' })
  })

  it('handles revoking user sessions with confirmation dialog', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const memberRow = w.findAll('[data-testid="user-row"]')[1] // Alice
    const revokeBtn = memberRow.find('[data-testid="action-revoke-sessions"]')
    expect(revokeBtn.exists()).toBe(true)

    await revokeBtn.trigger('click')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/user-member-2/sessions/revoke', {
      method: 'POST'
    })
  })

  it('opens password reset modal and submits new password', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const memberRow = w.findAll('[data-testid="user-row"]')[1] // Alice
    const resetPwBtn = memberRow.find('[data-testid="action-reset-password"]')
    expect(resetPwBtn.exists()).toBe(true)

    await resetPwBtn.trigger('click')
    await nextTick()

    const pwInput = w.find('[data-testid="new-password-input"]')
    expect(pwInput.exists()).toBe(true)

    const submitBtn = w.find('[data-testid="save-password-button"]')
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await pwInput.setValue('supersecret123')
    await nextTick()
    expect(submitBtn.attributes('disabled')).toBeUndefined()

    await submitBtn.trigger('click')
    await nextTick()

    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/user-member-2/password', {
      method: 'POST',
      json: { password: 'supersecret123' }
    })
    // Modal should close
    expect(w.find('[data-testid="new-password-input"]').exists()).toBe(false)
  })

  it('displays kick from voice action only when user is in a voice channel', async () => {
    const voiceStore = useVoiceStore()
    voiceStore.channelUsers = {
      'ch-voice-1': {
        'user-member-2': { id: 'user-member-2', username: 'alice' }
      }
    }

    const w = await createWrapper()
    await nextTick()
    await nextTick()

    const aliceRow = w.findAll('[data-testid="user-row"]')[1]
    const kickBtn = aliceRow.find('[data-testid="action-kick"]')
    expect(kickBtn.exists()).toBe(true)

    await kickBtn.trigger('click')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/user-member-2/kick', {
      method: 'POST'
    })

    const bobRow = w.findAll('[data-testid="user-row"]')[2]
    expect(bobRow.find('[data-testid="action-kick"]').exists()).toBe(false)
  })
})

describe('AdminDashboard - Channels & Layout Tab', () => {
  it('lists categories and channels and allows reordering and saving layout', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    await w.find('[data-testid="tab-channels"]').trigger('click')
    await nextTick()

    expect(w.find('[data-testid="category-item-cat-1"]').exists()).toBe(true)
    expect(w.find('[data-testid="category-item-cat-2"]').exists()).toBe(true)
    expect(w.find('[data-testid="channel-item-ch-1"]').exists()).toBe(true)
    expect(w.find('[data-testid="channel-item-ch-2"]').exists()).toBe(true)

    // Move channel down
    const cat1 = w.find('[data-testid="category-item-cat-1"]')
    const moveDownBtn = cat1.find('[data-testid="move-down-channel"]')
    await moveDownBtn.trigger('click')
    await nextTick()

    // Save layout
    const saveLayoutBtn = w.find('[data-testid="save-layout-button"]')
    await saveLayoutBtn.trigger('click')
    await nextTick()

    expect(apiMock).toHaveBeenCalledWith('/api/admin/layout', expect.objectContaining({
      method: 'PUT'
    }))
  })

  it('allows renaming a channel via edit dialog', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    await w.find('[data-testid="tab-channels"]').trigger('click')
    await nextTick()

    const ch1 = w.find('[data-testid="channel-item-ch-1"]')
    await ch1.find('[data-testid="rename-channel"]').trigger('click')
    await nextTick()

    const nameInput = w.find('input[placeholder="e.g. general"]')
    expect(nameInput.exists()).toBe(true)
    await nameInput.setValue('general-chat')

    const saveBtn = w.findAll('button').find(b => b.text() === 'Save')
    await saveBtn.trigger('click')
    await nextTick()

    expect(apiMock).toHaveBeenCalledWith('/api/admin/channels/ch-1', {
      method: 'PATCH',
      json: { name: 'general-chat', topic: 'Chatting' }
    })
  })

  it('allows deleting a channel with confirm', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    await w.find('[data-testid="tab-channels"]').trigger('click')
    await nextTick()

    const ch1 = w.find('[data-testid="channel-item-ch-1"]')
    await ch1.find('[data-testid="delete-channel"]').trigger('click')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/channels/ch-1', {
      method: 'DELETE'
    })
  })

  it('allows deleting a category with confirm', async () => {
    const w = await createWrapper()
    await nextTick()
    await nextTick()

    await w.find('[data-testid="tab-channels"]').trigger('click')
    await nextTick()

    const cat1 = w.find('[data-testid="category-item-cat-1"]')
    await cat1.find('[data-testid="delete-category"]').trigger('click')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/categories/cat-1', {
      method: 'DELETE'
    })
  })
})

async function openChannelsTab() {
  const w = await createWrapper()
  await w.find('[data-testid="tab-channels"]').trigger('click')
  await flushPromises()
  return w
}

const channelIds = (w, catId) =>
  w.find(`[data-testid="category-item-${catId}"]`).findAll('[data-testid^="channel-item-"]').map(el => el.attributes('data-testid').replace('channel-item-', ''))
const categoryIds = w =>
  w.findAll('[data-testid^="category-item-"]').map(el => el.attributes('data-testid').replace('category-item-', ''))
const unsavedBanner = w => w.text().includes('Unsaved changes to channel order.')

describe('AdminDashboard - password policy', () => {
  it('requires the server minimum length before a reset can be sent', async () => {
    const w = await createWrapper()
    await w.findAll('[data-testid="user-row"]')[1].find('[data-testid="action-reset-password"]').trigger('click')
    const input = w.find('[data-testid="new-password-input"]')
    const submit = () => w.find('[data-testid="save-password-button"]')

    await input.setValue('x'.repeat(MIN_PASSWORD_LENGTH - 1))
    expect(submit().attributes('disabled')).toBeDefined()
    await w.find('form').trigger('submit')
    expect(apiMock).not.toHaveBeenCalledWith('/api/admin/users/user-member-2/password', expect.anything())

    await input.setValue('x'.repeat(MIN_PASSWORD_LENGTH))
    expect(submit().attributes('disabled')).toBeUndefined()
  })
})

describe('AdminDashboard - unsaved layout', () => {
  it('keeps an unsaved reorder when a channel is renamed', async () => {
    const w = await openChannelsTab()
    await w.find('[data-testid="category-item-cat-1"]').find('[data-testid="move-down-channel"]').trigger('click')
    expect(channelIds(w, 'cat-1')).toEqual(['ch-2', 'ch-1'])

    await w.find('[data-testid="channel-item-ch-1"]').find('[data-testid="rename-channel"]').trigger('click')
    await w.find('input[placeholder="e.g. general"]').setValue('general-chat')
    await w.findAll('button').find(b => b.text() === 'Save').trigger('click')
    await flushPromises()

    expect(channelIds(w, 'cat-1')).toEqual(['ch-2', 'ch-1'])
    expect(w.find('[data-testid="channel-item-ch-1"]').text()).toContain('general-chat')
    expect(unsavedBanner(w)).toBe(true)

    await w.find('[data-testid="save-layout-button"]').trigger('click')
    const [, layout] = apiMock.mock.calls.find(([url]) => url === '/api/admin/layout')
    expect(layout.json.channels.filter(c => c.category_id === 'cat-1')).toEqual([
      { id: 'ch-2', category_id: 'cat-1', sort_order: 0 },
      { id: 'ch-1', category_id: 'cat-1', sort_order: 1 }
    ])
  })

  it('keeps an unsaved reorder when a category is deleted and moves its channels to uncategorized', async () => {
    const w = await openChannelsTab()
    await w.find('[data-testid="category-item-cat-1"]').find('[data-testid="move-down-channel"]').trigger('click')

    await w.find('[data-testid="category-item-cat-2"]').find('[data-testid="delete-category"]').trigger('click')
    await flushPromises()

    expect(categoryIds(w)).toEqual(['cat-1'])
    expect(channelIds(w, 'cat-1')).toEqual(['ch-2', 'ch-1'])
    expect(w.find('[data-testid="channel-item-ch-voice-1"]').exists()).toBe(true)
    expect(unsavedBanner(w)).toBe(true)
  })

  it('reloads from the server after an edit when nothing is pending', async () => {
    const w = await openChannelsTab()
    apiMock.mockClear()
    await w.find('[data-testid="channel-item-ch-1"]').find('[data-testid="delete-channel"]').trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.filter(([url]) => url === '/api/channels').length).toBeGreaterThanOrEqual(2)
    expect(unsavedBanner(w)).toBe(false)
  })

  it('keeps an unsaved reorder across a tab switch', async () => {
    const w = await openChannelsTab()
    await w.find('[data-testid="move-down-category"]').trigger('click')
    expect(categoryIds(w)).toEqual(['cat-2', 'cat-1'])

    await w.find('[data-testid="tab-users"]').trigger('click')
    await w.find('[data-testid="tab-channels"]').trigger('click')
    expect(categoryIds(w)).toEqual(['cat-2', 'cat-1'])
    expect(unsavedBanner(w)).toBe(true)
  })
})

describe('AdminDashboard - drag and drop', () => {
  it('forgets a cancelled category drag', async () => {
    const w = await openChannelsTab()
    const cat1 = w.find('[data-testid="category-item-cat-1"]')
    const cat2 = w.find('[data-testid="category-item-cat-2"]')

    await cat1.trigger('dragstart')
    await cat1.trigger('dragend')
    await cat2.trigger('drop')

    expect(categoryIds(w)).toEqual(['cat-1', 'cat-2'])
    expect(unsavedBanner(w)).toBe(false)
  })

  it('forgets a cancelled channel drag', async () => {
    const w = await openChannelsTab()
    const ch1 = w.find('[data-testid="channel-item-ch-1"]')

    await ch1.trigger('dragstart')
    await ch1.trigger('dragend')
    await w.find('[data-testid="channel-item-ch-voice-1"]').trigger('drop')

    expect(channelIds(w, 'cat-1')).toEqual(['ch-1', 'ch-2'])
    expect(unsavedBanner(w)).toBe(false)
  })

  it('drops a category drag when a channel drag starts', async () => {
    const w = await openChannelsTab()
    const cat1 = w.find('[data-testid="category-item-cat-1"]')
    await cat1.trigger('dragstart')
    await w.find('[data-testid="channel-item-ch-1"]').trigger('dragstart')
    await w.find('[data-testid="category-item-cat-2"]').trigger('drop')

    expect(categoryIds(w)).toEqual(['cat-1', 'cat-2'])
  })

  it('still moves a category by drag and drop', async () => {
    const w = await openChannelsTab()
    await w.find('[data-testid="category-item-cat-1"]').trigger('dragstart')
    await w.find('[data-testid="category-item-cat-2"]').trigger('drop')
    expect(categoryIds(w)).toEqual(['cat-2', 'cat-1'])
    expect(unsavedBanner(w)).toBe(true)
  })

  it('moves a channel into another category and saves the whole layout', async () => {
    const w = await openChannelsTab()
    await w.find('[data-testid="channel-item-ch-uncat-1"]').trigger('dragstart')
    await w.find('[data-testid="channel-item-ch-2"]').trigger('drop')
    expect(channelIds(w, 'cat-1')).toEqual(['ch-1', 'ch-uncat-1', 'ch-2'])
    expect(w.find('[data-testid="channel-item-ch-uncat-1"]').exists()).toBe(true)

    await w.find('[data-testid="channel-item-ch-1"]').trigger('dragstart')
    await w.find('[data-testid="category-item-cat-2"]').find('.border-l-2').trigger('drop')
    expect(channelIds(w, 'cat-2')).toEqual(['ch-voice-1', 'ch-1'])

    await w.find('[data-testid="save-layout-button"]').trigger('click')
    const [, layout] = apiMock.mock.calls.find(([url]) => url === '/api/admin/layout')
    expect(layout.json).toEqual({
      categories: [{ id: 'cat-1', sort_order: 0 }, { id: 'cat-2', sort_order: 1 }],
      channels: [
        { id: 'ch-uncat-1', category_id: 'cat-1', sort_order: 0 },
        { id: 'ch-2', category_id: 'cat-1', sort_order: 1 },
        { id: 'ch-voice-1', category_id: 'cat-2', sort_order: 0 },
        { id: 'ch-1', category_id: 'cat-2', sort_order: 1 }
      ]
    })
  })

  it('ignores a drop on the channel being dragged', async () => {
    const w = await openChannelsTab()
    const ch1 = w.find('[data-testid="channel-item-ch-1"]')
    await ch1.trigger('dragstart')
    await ch1.trigger('drop')
    expect(channelIds(w, 'cat-1')).toEqual(['ch-1', 'ch-2'])
    expect(unsavedBanner(w)).toBe(false)
  })
})

describe('AdminDashboard - load errors', () => {
  it('reports each failed request and keeps the data already shown', async () => {
    const w = await createWrapper()
    await w.find('[data-testid="tab-media"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('pic.png')
    expect(w.text()).toContain('1 MB')

    const toasts = useToastStore()
    apiMock.mockImplementationOnce(() => Promise.reject(new Error('stats down')))
    apiMock.mockImplementationOnce(() => Promise.reject(new Error('media down')))
    await w.find('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()

    expect(toasts.toasts.filter(t => t.type === 'error').map(t => t.text)).toEqual(['stats down', 'media down'])
    expect(w.text()).toContain('pic.png')
    expect(w.text()).toContain('1 MB')
  })

  it('shows a toast when the user list cannot be loaded', async () => {
    apiMock.mockImplementationOnce(() => Promise.reject(new Error('users down')))
    const w = await createWrapper()
    const toasts = useToastStore()
    expect(toasts.toasts.map(t => t.text)).toContain('users down')
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(0)
  })
})
