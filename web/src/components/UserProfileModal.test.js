import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import UserProfileModal from './UserProfileModal.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'

const mockApi = vi.fn()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal()),
  api: (...args) => mockApi(...args)
}))

let wrapper
let auth
let chat

beforeEach(() => {
  mockApi.mockReset()
  setActivePinia(createPinia())
  auth = useAuthStore()
  chat = useChatStore()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

const stub = { id: 'u1', username: 'ada', display_name: 'Ada', role: 'user', created_at: null }
const editButton = () => {
  const b = wrapper.find('[data-testid="profile-bio-edit"]')
  return b.exists() ? b : undefined
}
const saveButton = () => wrapper.findAll('button').find(b => b.text() === 'Speichern')
const cancelButton = () => wrapper.findAll('button').find(b => b.text() === 'Abbrechen')

describe('UserProfileModal bio editing', () => {
  it('fills the edit fields from the loaded account, not the profile stub', async () => {
    auth.user = { ...stub, display_name: 'Ada L.', bio: 'Writes engines.' }
    chat.selectedUserProfile = { ...stub } // opened from a message: no bio yet
    wrapper = mount(UserProfileModal, { attachTo: document.body })

    await editButton().trigger('click')
    expect(wrapper.find('#profile-bio').element.value).toBe('Writes engines.')
    expect(wrapper.find('#profile-displayname').element.value).toBe('Ada L.')

    mockApi.mockResolvedValue({ ...auth.user })
    await saveButton().trigger('click')
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/profile', expect.objectContaining({
      method: 'PUT',
      json: { display_name: 'Ada L.', bio: 'Writes engines.' }
    }))
  })

  it('uses the bio that arrives after the profile opened', async () => {
    auth.user = { ...stub } // no bio known for the account
    chat.selectedUserProfile = { ...stub }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(editButton()).toBeUndefined()

    chat.selectedUserProfile = { ...stub, bio: 'Loaded later.' }
    await nextTick()
    await editButton().trigger('click')
    expect(wrapper.find('#profile-bio').element.value).toBe('Loaded later.')
  })

  it('discards unsaved edits on cancel and starts fresh next time', async () => {
    auth.user = { ...stub, bio: 'Original.' }
    wrapper = mount(UserProfileModal, { attachTo: document.body })

    await editButton().trigger('click')
    await wrapper.find('#profile-bio').setValue('Draft')
    await cancelButton().trigger('click')
    expect(mockApi).not.toHaveBeenCalled()

    await editButton().trigger('click')
    expect(wrapper.find('#profile-bio').element.value).toBe('Original.')
  })

  it('offers no editing on someone else\'s profile', async () => {
    auth.user = { ...stub, bio: 'Mine.' }
    wrapper = mount(UserProfileModal, { attachTo: document.body, props: { user: { id: 'u2', username: 'zoe', bio: 'Theirs.' } } })
    expect(editButton()).toBeUndefined()
  })
})

it('matches the server password minimum', () => {
  expect(MIN_PASSWORD_LENGTH).toBe(10)
})

describe('UserProfileModal password change', () => {
  it('rejects passwords shorter than the server minimum', async () => {
    auth.user = { ...stub, bio: '' }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    await wrapper.findAll('button').find(b => b.text() === 'Ändern').trigger('click')
    const input = wrapper.find('input[autocomplete="new-password"]')
    expect(input.attributes('placeholder')).toBe(`Neues Passwort (mind. ${MIN_PASSWORD_LENGTH} Zeichen)`)
    await wrapper.find('input[autocomplete="current-password"]').setValue('old-password')
    await input.setValue('a'.repeat(MIN_PASSWORD_LENGTH - 1))
    await wrapper.findAll('input[autocomplete="new-password"]')[1].setValue('a'.repeat(MIN_PASSWORD_LENGTH - 1))
    await wrapper.find('form').trigger('submit')
    expect(wrapper.find('[role="alert"]').text()).toBe(`Das neue Passwort braucht mindestens ${MIN_PASSWORD_LENGTH} Zeichen.`)
    expect(mockApi).not.toHaveBeenCalled()
  })
})
