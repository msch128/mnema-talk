import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminEditDialogs from './AdminEditDialogs.vue'
import { i18nPlugin, setLocale } from '../i18n'
import { pendingConfirm } from '../lib/confirm'
import { categoryFixture, channelFixture } from '../test-fixtures.fixture'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  submit.mockReset()
  submit.mockResolvedValue(undefined)
})

const submit = vi.fn(async (_fields: { name: string; topic: string }) => {})
const mountDialog = (props: Record<string, unknown>) => mount(AdminEditDialogs, { props: { submit, ...props }, global: { plugins: [i18nPlugin] } })
const saveButton = (w: ReturnType<typeof mountDialog>) => w.get<HTMLButtonElement>('[data-testid="unsaved-save"]').element

describe('AdminEditDialogs', () => {
  it('renames a category: Save is off until the trimmed name differs, and the trimmed name is handed back', async () => {
    const w = mountDialog({ category: categoryFixture({ name: 'Projects' }) })
    expect(saveButton(w).disabled).toBe(true)
    await w.get('#admin-edit-name').setValue('  Projects ')
    expect(saveButton(w).disabled).toBe(true)
    await w.get('#admin-edit-name').setValue(' Work ')
    expect(saveButton(w).disabled).toBe(false)
    expect(submit).not.toHaveBeenCalled()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(submit).toHaveBeenCalledWith({ name: 'Work', topic: '' })
  })

  it('edits a channel name and topic together and refuses an empty name inline', async () => {
    const w = mountDialog({ channel: channelFixture({ name: 'general', topic: 'old' }) })
    await w.get('#admin-edit-topic').setValue('new topic ')
    await w.get('#admin-edit-name').setValue('  ')
    expect(w.get('[data-testid="admin-edit-error"]').text()).toBe('Enter a name.')
    expect(saveButton(w).disabled).toBe(true)
    await w.get('#admin-edit-name').setValue('lounge')
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(submit).toHaveBeenCalledWith({ name: 'lounge', topic: 'new topic' })
  })

  it('asks before closing with unsaved edits', async () => {
    const w = mountDialog({ category: categoryFixture({ name: 'Projects' }) })
    await w.get('[data-dialog-close]').trigger('click')
    await flushPromises()
    expect(w.emitted('close')).toHaveLength(1)
    await w.get('#admin-edit-name').setValue('Other')
    await w.get('[data-dialog-close]').trigger('click')
    await flushPromises()
    expect(pendingConfirm.value?.title).toBe('Discard changes?')
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(w.emitted('close')).toHaveLength(2)
  })

  it('shows a refused save inline and keeps the draft so it can be tried again', async () => {
    submit.mockRejectedValueOnce(new Error('edit denied'))
    const w = mountDialog({ category: categoryFixture({ name: 'Projects' }) })
    await w.get('#admin-edit-name').setValue('Work')
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="unsaved-error"]').text()).toBe('edit denied')
    expect(saveButton(w).disabled).toBe(false)
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(submit).toHaveBeenCalledTimes(2)
  })
})
