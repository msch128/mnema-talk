import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WelcomeProfileStep from './WelcomeProfileStep.vue'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale, t } from '../i18n'
import { userFixture } from '../test-fixtures.fixture'

function step() {
  return mount(WelcomeProfileStep, { global: { plugins: [i18nPlugin] } })
}

async function pick(w: ReturnType<typeof step>, file: File) {
  const input = w.get<HTMLInputElement>('[data-testid="welcome-avatar"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
  await flushPromises()
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ display_name: 'Ada', bio: 'hi' })
})
afterEach(() => { vi.restoreAllMocks() })

describe('WelcomeProfileStep', () => {
  it('continues without a request when nothing changed', async () => {
    const update = vi.spyOn(useAuthStore(), 'updateProfile')
    const w = step()
    await w.get('form').trigger('submit')
    expect(update).not.toHaveBeenCalled()
    expect(w.emitted('done')).toHaveLength(1)
  })

  it('saves a changed display name and bio, then continues', async () => {
    const update = vi.spyOn(useAuthStore(), 'updateProfile').mockResolvedValue(userFixture())
    const w = step()
    await w.get('#welcome-displayname').setValue('Ada L')
    await w.get('#welcome-bio').setValue('Maths')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith({ displayName: 'Ada L', bio: 'Maths' })
    expect(w.emitted('done')).toHaveLength(1)
  })

  it('stays on the step and says so when saving fails', async () => {
    vi.spyOn(useAuthStore(), 'updateProfile').mockRejectedValue(new Error('no'))
    const w = step()
    await w.get('#welcome-bio').setValue('Maths')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.emitted('done')).toBeUndefined()
    expect(useToastStore().toasts.at(-1)?.text).toBeTruthy()
  })

  it('can be skipped', async () => {
    const w = step()
    await w.get('[data-testid="welcome-skip"]').trigger('click')
    expect(w.emitted('skip')).toHaveLength(1)
  })

  it('makes the avatar itself the way to change it', async () => {
    const w = step()
    const click = vi.spyOn(w.get<HTMLInputElement>('[data-testid="welcome-avatar"]').element, 'click')
    const button = w.get('[data-testid="welcome-avatar-button"]')
    expect(button.attributes('aria-label')).toBe(t('profile.changeAvatar'))
    await button.trigger('click')
    expect(click).toHaveBeenCalledOnce()
  })

  it('uploads a chosen avatar', async () => {
    const upload = vi.spyOn(useAuthStore(), 'uploadAvatar').mockResolvedValue(userFixture())
    const file = new File(['x'], 'me.png', { type: 'image/png' })
    await pick(step(), file)
    expect(upload).toHaveBeenCalledWith(file)
  })

  it('refuses an image over 5 MB without uploading it', async () => {
    const upload = vi.spyOn(useAuthStore(), 'uploadAvatar')
    await pick(step(), new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'big.png', { type: 'image/png' }))
    expect(upload).not.toHaveBeenCalled()
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('profile.avatarTooBig', { size: 5 }))
  })

  it('reports a failed upload', async () => {
    vi.spyOn(useAuthStore(), 'uploadAvatar').mockRejectedValue(new Error('no'))
    await pick(step(), new File(['x'], 'me.png', { type: 'image/png' }))
    expect(useToastStore().toasts.at(-1)?.text).toBeTruthy()
  })

  it('ignores a cancelled file dialog', async () => {
    const upload = vi.spyOn(useAuthStore(), 'uploadAvatar')
    const w = step()
    const input = w.get<HTMLInputElement>('[data-testid="welcome-avatar"]')
    Object.defineProperty(input.element, 'files', { value: [], configurable: true })
    await input.trigger('change')
    expect(upload).not.toHaveBeenCalled()
  })
})
