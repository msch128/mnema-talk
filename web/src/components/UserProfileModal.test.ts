import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import { userFixture, channelFixture, categoryFixture, voiceUserFixture, requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick, defineComponent, h } from 'vue'
import UserProfileModal from './UserProfileModal.vue'
import BaseDialog from './BaseDialog.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { t, setLocale } from '../i18n'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

let wrapper: VueWrapper
let auth: ReturnType<typeof useAuthStore>
let chat: ReturnType<typeof useChatStore>

beforeEach(() => {
  setLocale('de')
  mockApi.mockReset()
  setActivePinia(createPinia())
  auth = useAuthStore()
  chat = useChatStore()
})

afterEach(() => {
  wrapper?.unmount()
  // Wrapper was unmounted above.
  document.body.innerHTML = ''
})

const stub = { bio: '', status_text: '', locale: 'en', created_at: '2026-01-01T00:00:00Z',  id: "00000000-0000-4000-8000-0000000003e8", username: 'ada', display_name: 'Ada', role: 'user' as const }
const editButton = () => {
  const b = wrapper.find('[data-testid="profile-bio-edit"]')!
  return b.exists() ? b : undefined
}
const saveButton = () => wrapper.findAll('button').find(b => b.text() === 'Speichern')!
const cancelButton = () => wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!

describe('UserProfileModal bio editing', () => {
  it('fills the edit fields from the loaded account, not the profile stub', async () => {
    auth.user = userFixture({ ...stub, display_name: 'Ada L.', bio: 'Writes engines.' })
    chat.selectedUserProfile = { ...stub } // opened from a message: no bio yet
    wrapper = mount(UserProfileModal, { attachTo: document.body })

    await editButton()!.trigger('click')
    expect(wrapper.find<HTMLInputElement>('#profile-bio')!.element.value).toBe('Writes engines.')
    expect(wrapper.find<HTMLInputElement>('#profile-displayname')!.element.value).toBe('Ada L.')

    mockApi.mockResolvedValue({ ...auth.user })
    await saveButton()!.trigger('click')
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/profile', expect.objectContaining({
      method: 'PUT',
      json: { display_name: 'Ada L.', bio: 'Writes engines.' }
    }))
  })

  it('enables editing when the complete account arrives after the profile opened', async () => {
    auth.user = null // The complete account is still loading.
    chat.selectedUserProfile = { ...stub }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(editButton()).toBeUndefined()

    chat.selectedUserProfile = { ...stub, bio: 'Loaded later.' }
    auth.user = userFixture({ ...stub, bio: 'Loaded later.' })
    await nextTick()
    await editButton()!.trigger('click')
    expect(wrapper.find<HTMLInputElement>('#profile-bio')!.element.value).toBe('Loaded later.')
  })

  it('discards unsaved edits on cancel and starts fresh next time', async () => {
    auth.user = userFixture({ ...stub, bio: 'Original.' })
    wrapper = mount(UserProfileModal, { attachTo: document.body })

    await editButton()!.trigger('click')
    await wrapper.find<HTMLInputElement>('#profile-bio')!.setValue('Draft')
    await cancelButton().trigger('click')
    expect(mockApi).not.toHaveBeenCalled()

    await editButton()!.trigger('click')
    expect(wrapper.find<HTMLInputElement>('#profile-bio')!.element.value).toBe('Original.')
  })

  it('offers no editing on someone else\'s profile', async () => {
    auth.user = userFixture({ ...stub, bio: 'Mine.' })
    wrapper = mount(UserProfileModal, { attachTo: document.body, props: { user: { id: "00000000-0000-4000-8000-0000000003e9", username: 'zoe', bio: 'Theirs.' } } })
    expect(editButton()).toBeUndefined()
  })
})

it('matches the server password minimum', () => {
  expect(MIN_PASSWORD_LENGTH).toBe(10)
})

describe('UserProfileModal password change', () => {
  it('rejects passwords shorter than the server minimum', async () => {
    auth.user = userFixture({ ...stub, bio: '' })
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    await wrapper.findAll('button').find(b => b.text() === 'Ändern')!.trigger('click')
    const input = wrapper.find("input[autocomplete=\"new-password\"]")!
    expect(input.attributes('placeholder')).toBe(`Neues Passwort (mind. ${MIN_PASSWORD_LENGTH} Zeichen)`)
    await wrapper.find('input[autocomplete="current-password"]')!.setValue('old-password')
    await input.setValue('a'.repeat(MIN_PASSWORD_LENGTH - 1))
    await wrapper.findAll("input[autocomplete=\"new-password\"]")[1]!.setValue('a'.repeat(MIN_PASSWORD_LENGTH - 1))
    await wrapper.find('form')!.trigger('submit')
    expect(wrapper.find('[role="alert"]')!.text()).toBe(`Das neue Passwort braucht mindestens ${MIN_PASSWORD_LENGTH} Zeichen.`)
    expect(mockApi).not.toHaveBeenCalled()
  })
})

describe('UserProfileModal account interactions', () => {
  function openSelf() {
    auth.user = userFixture({ ...stub })
    wrapper = mount(UserProfileModal, { attachTo: document.body })
  }

  function button(key: Parameters<typeof t>[0]) {
    return requireValue(wrapper.findAll('button').find(b => b.text() === t(key)), `Missing ${key} button`)
  }

  async function passwords(password: string, repeated = password) {
    await button('profile.changePassword').trigger('click')
    await wrapper.get('input[autocomplete="current-password"]').setValue('previous-password')
    await wrapper.get('input[autocomplete="new-password"]').setValue(password)
    await requireValue(wrapper.findAll('input[autocomplete="new-password"]')[1]).setValue(repeated)
  }

  it('saves edited profile fields into the loaded profile and announces success', async () => {
    openSelf()
    chat.selectedUserProfile = { ...stub }
    await wrapper.get('[data-testid="profile-bio-edit"]').trigger('click')
    await wrapper.get('#profile-bio').setValue('Revised biography')
    await wrapper.get('#profile-displayname').setValue('Ada Revised')
    mockApi.mockResolvedValue(userFixture({ ...stub, bio: 'Revised biography', display_name: 'Ada Revised' }))
    await saveButton().trigger('click')
    await flushPromises()
    expect(chat.selectedUserProfile?.bio).toBe('Revised biography')
    expect(wrapper.find('#profile-bio').exists()).toBe(false)
  })

  it.each([new Error('Profile request failed'), null])('keeps a rejected profile edit available to retry (%s)', async error => {
    openSelf()
    await wrapper.get('[data-testid="profile-bio-edit"]').trigger('click')
    mockApi.mockRejectedValue(error)
    await saveButton().trigger('click')
    await flushPromises()
    expect(wrapper.find('#profile-bio').exists()).toBe(true)
    expect(wrapper.text()).toContain(error instanceof Error ? error.message : t('profile.saveFailed'))
    expect(saveButton().attributes('disabled')).toBeUndefined()
  })

  it('does not save an edit after the signed-in account disappears', async () => {
    openSelf()
    await wrapper.get('[data-testid="profile-bio-edit"]').trigger('click')
    auth.user = null
    await nextTick()
    await saveButton().trigger('click')
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('rejects repeated-password mismatch before contacting the server', async () => {
    openSelf()
    await passwords('long-password', 'different-password')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toBe(t('profile.passwordMismatch'))
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('submits a matching password and closes the password editor', async () => {
    openSelf()
    await passwords('valid-password')
    mockApi.mockResolvedValue({ ok: true })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/auth/password', expect.objectContaining({
      json: { current_password: 'previous-password', new_password: 'valid-password' }
    }))
    expect(wrapper.find('input[autocomplete="current-password"]').exists()).toBe(false)
  })

  it.each([new Error('Wrong current password'), {}])('shows password failures and allows retry (%s)', async error => {
    openSelf()
    await passwords('valid-password')
    mockApi.mockRejectedValue(error)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(error instanceof Error ? error.message : t('profile.saveFailed'))
    expect(saveButton().attributes('disabled')).toBeUndefined()
  })

  it('cancels the password form without making a request', async () => {
    openSelf()
    await passwords('valid-password')
    await button('common.cancel').trigger('click')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('edits and trims the status line', async () => {
    openSelf()
    chat.selectedUserProfile = { ...stub }
    await wrapper.get('[data-testid="profile-status-edit"]').trigger('click')
    await wrapper.get('[data-testid="profile-status-input"]').setValue('  Available later  ')
    mockApi.mockResolvedValue(userFixture({ ...stub, status_text: 'Available later' }))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/status', expect.objectContaining({ json: { status_text: 'Available later' } }))
    expect(chat.selectedUserProfile?.status_text).toBe('Available later')
    expect(wrapper.find('[data-testid="profile-status-input"]').exists()).toBe(false)
  })

  it('lets an administrator clear another member status', async () => {
    auth.user = userFixture({ role: 'admin' })
    const other = userFixture({ ...stub, status_text: 'Busy' })
    chat.selectedUserProfile = other
    wrapper = mount(UserProfileModal, { attachTo: document.body, props: { user: other } })
    mockApi.mockResolvedValue({ ...other, status_text: '' })
    await wrapper.get('[data-testid="profile-status-clear"]').trigger('click')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith(`/api/admin/users/${other.id}/status`, expect.objectContaining({ json: { status_text: '' } }))
    expect(chat.selectedUserProfile?.status_text).toBe('')
  })

  it.each([new Error('Status request failed'), false])('retains status text after failure (%s)', async error => {
    openSelf()
    await wrapper.get('[data-testid="profile-status-edit"]').trigger('click')
    await wrapper.get('[data-testid="profile-status-input"]').setValue('Retry this status')
    mockApi.mockRejectedValue(error)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="profile-status-input"]').element.value).toBe('Retry this status')
    expect(saveButton().attributes('disabled')).toBeUndefined()
  })

  it('forwards Escape dismissal from the enclosing dialog', async () => {
    openSelf()
    await wrapper.get('[data-testid="profile-status-edit"]').trigger('click')
    wrapper.get('[data-testid="profile-status-input"]').element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    expect(wrapper.emitted('close')).toEqual([[]])
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('cancels the local status input on Escape when rendered without the enclosing focus trap', async () => {
    auth.user = userFixture({ ...stub })
    wrapper = mount(UserProfileModal, { attachTo: document.body, global: { stubs: {
      BaseDialog: defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.({ titleId: 'profile-unit-title' })) } })
    } } })
    await wrapper.get('[data-testid="profile-status-edit"]').trigger('click')
    await wrapper.get('[data-testid="profile-status-input"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="profile-status-input"]').exists()).toBe(false)
  })

  it('forwards both child dismissal and the close button', async () => {
    openSelf()
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await wrapper.get('[data-dialog-close]').trigger('click')
    expect(wrapper.emitted('close')).toEqual([[], []])
  })

  it('opens the avatar picker from the avatar itself but does not open it for another member', async () => {
    openSelf()
    const click = vi.spyOn(wrapper.get<HTMLInputElement>('input[type="file"]').element, 'click')
    await wrapper.get('.cursor-pointer').trigger('click')
    expect(click).toHaveBeenCalledOnce()
    await wrapper.setProps({ user: userFixture({ username: 'other-member', avatar_url: '/api/media/synthetic-avatar' }) })
    await wrapper.get('.cursor-pointer').trigger('click')
    expect(click).toHaveBeenCalledOnce()
    expect(wrapper.get('img').attributes('alt')).toBe(t('user.avatar'))
  })

  it('renders an administrator profile with its badge and uncategorized voice channel', () => {
    auth.user = userFixture({ ...stub, role: 'admin' })
    const voice = useVoiceStore()
    const channel = channelFixture({ type: 'voice', name: 'Lobby' })
    chat.uncategorized = [channel]
    voice.channelUsers = { unrelated: { another: voiceUserFixture() }, [channel.id]: { [stub.id]: voiceUserFixture({ ...stub }) } }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.text()).toContain('#Lobby')
    expect(wrapper.find(`[aria-label="${t('profile.adminHint')}"]`).exists()).toBe(true)
  })

  it('does not submit a status editor opened for a partial profile without an identifier', async () => {
    auth.user = userFixture({ role: 'admin' })
    wrapper = mount(UserProfileModal, { attachTo: document.body, props: { user: { username: 'loading-member' } } })
    await wrapper.get('[data-testid="profile-status-edit"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('saves a profile without a separate selected profile record', async () => {
    openSelf()
    await wrapper.get('[data-testid="profile-bio-edit"]').trigger('click')
    mockApi.mockResolvedValue(userFixture({ ...stub }))
    await saveButton().trigger('click')
    await flushPromises()
    expect(chat.selectedUserProfile).toBeNull()
    expect(wrapper.find('#profile-bio').exists()).toBe(false)
  })

  it('ignores an edit click queued before logout removes the button', async () => {
    openSelf()
    await wrapper.setProps({ user: { id: stub.id, username: stub.username } })
    const edit = wrapper.get('[data-testid="profile-bio-edit"]')
    auth.user = null
    await edit.trigger('click')
    expect(wrapper.find('#profile-bio').exists()).toBe(false)
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('finishes an avatar upload safely if logout removes the file input', async () => {
    openSelf()
    let finish: ((value: unknown) => void) | undefined
    mockApi.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['image'], 'avatar.png')] })
    await input.trigger('change')
    auth.user = null
    await nextTick()
    requireValue(finish)(userFixture({ ...stub }))
    await flushPromises()
    expect(chat.selectedUserProfile).toBeNull()
  })

  it('mentions another member and closes the profile', async () => {
    wrapper = mount(UserProfileModal, { attachTo: document.body, props: { user: { username: 'ada' } } })
    await button('profile.mention').trigger('click')
    expect(wrapper.emitted('mention')).toEqual([['ada']])
    expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('shows a safe empty profile and ignores a mention without a username', async () => {
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    await button('profile.mention').trigger('click')
    expect(wrapper.emitted('mention')).toBeUndefined()
    expect(wrapper.text()).toContain(t('profile.unknown'))
  })

  it('renders own chosen presence and the current categorized voice channel', () => {
    auth.user = userFixture({ ...stub, presence: 'focus' })
    const voice = useVoiceStore()
    const channel = channelFixture({ type: 'voice', name: 'Workshop' })
    chat.categories = [categoryFixture({ channels: [channel] })]
    voice.channelUsers = { [channel.id]: { [stub.id]: voiceUserFixture({ ...stub }) } }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.text()).toContain('#Workshop')
    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(t('presence.focus'))
  })

  it('falls back to a generic name for a voice channel absent from the channel hierarchy', () => {
    auth.user = userFixture({ ...stub })
    useVoiceStore().channelUsers = { unknown: { [stub.id]: voiceUserFixture({ ...stub }) } }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.text()).toContain(`#${t('profile.talk')}`)
  })

  it('uses the original join date when formatting rejects an invalid timestamp', () => {
    wrapper = mount(UserProfileModal, { attachTo: document.body, props: { user: { created_at: 'Invalid date' } } })
    expect(wrapper.text()).toContain('Invalid date')
  })

  it('opens the native avatar picker only for the signed-in account', async () => {
    openSelf()
    const click = vi.spyOn(wrapper.get<HTMLInputElement>('input[type="file"]').element, 'click')
    await button('profile.changeAvatar').trigger('click')
    expect(click).toHaveBeenCalledOnce()
    await wrapper.get('input[type="file"]').trigger('change')
    expect(mockApi).not.toHaveBeenCalled()
  })

  async function chooseFile(file: File) {
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    await flushPromises()
  }

  it('rejects oversized avatars before uploading', async () => {
    openSelf()
    await chooseFile(new File([new Uint8Array(10 * 1024 * 1024 + 1)], 'large.png', { type: 'image/png' }))
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('uploads the chosen avatar and updates the loaded profile', async () => {
    openSelf()
    chat.selectedUserProfile = { ...stub }
    const file = new File(['image'], 'avatar.png', { type: 'image/png' })
    mockApi.mockResolvedValue(userFixture({ ...stub, avatar_url: '/api/media/synthetic-avatar' }))
    await chooseFile(file)
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/avatar', expect.objectContaining({ method: 'POST', form: expect.any(FormData) }))
    expect(chat.selectedUserProfile?.avatar_url).toBe('/api/media/synthetic-avatar')
    expect(wrapper.get<HTMLInputElement>('input[type="file"]').element.value).toBe('')
  })

  it.each([new Error('Avatar rejected'), undefined])('allows another avatar upload after failure (%s)', async error => {
    openSelf()
    mockApi.mockRejectedValue(error)
    await chooseFile(new File(['image'], 'avatar.png', { type: 'image/png' }))
    expect(button('profile.changeAvatar').attributes('disabled')).toBeUndefined()
  })
})
