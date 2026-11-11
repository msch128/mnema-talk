import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ForumSettingsForm from './ForumSettingsForm.vue'
import { setLocale } from '../i18n'
import * as forumApi from '../lib/forumApi'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../shared/lib/confirm'

vi.mock('../lib/forumApi', () => ({ updateForumSettings: vi.fn() }))

function mountForm(settings = { require_tag: false, guidelines: '' }, noTags = false) {
  return mount(ForumSettingsForm, { props: { channelId: 'c1', settings, noTags } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
})

describe('ForumSettingsForm', () => {
  it('can only be saved after a change, and a change back disables it again', async () => {
    const w = mountForm()
    const save = () => w.get('[data-testid="forum-settings-save"]')
    expect(save().attributes('disabled')).toBeDefined()
    await w.get('#forum-guidelines').setValue('Rules')
    expect(save().attributes('disabled')).toBeUndefined()
    await w.get('#forum-guidelines').setValue('')
    expect(save().attributes('disabled')).toBeDefined()
  })

  it('follows new settings from outside and says nothing is required without tags', async () => {
    const w = mountForm({ require_tag: false, guidelines: '' }, true)
    expect(w.text()).toContain('Add a tag first')
    await w.setProps({ settings: { require_tag: true, guidelines: 'New' } })
    expect((w.get('#forum-guidelines').element as HTMLTextAreaElement).value).toBe('New')
    expect(w.get('[data-testid="forum-require-tag"]').attributes('aria-checked')).toBe('true')
  })

  it('keeps an unsaved edit when fresh settings arrive', async () => {
    const w = mountForm()
    await w.get('#forum-guidelines').setValue('Draft rules')
    await w.setProps({ settings: { require_tag: true, guidelines: '' } })
    expect((w.get('#forum-guidelines').element as HTMLTextAreaElement).value).toBe('Draft rules')
  })

  it('reports a failed save inline and keeps the form dirty', async () => {
    vi.mocked(forumApi.updateForumSettings).mockRejectedValue(new Error('refused'))
    const w = mountForm()
    await w.get('[data-testid="forum-require-tag"]').trigger('click')
    await w.get('[data-testid="forum-settings-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="unsaved-error"]').text()).toContain('refused')
    expect(w.emitted('saved')).toBeUndefined()
    expect(w.get('[data-testid="forum-settings-save"]').attributes('disabled')).toBeUndefined()
  })

  it('saves trimmed guidelines, confirms with a toast and is clean again', async () => {
    vi.mocked(forumApi.updateForumSettings).mockResolvedValue({ require_tag: false, guidelines: 'Rules' })
    const w = mountForm()
    await w.get('#forum-guidelines').setValue('  Rules  ')
    await w.get('[data-testid="forum-settings-save"]').trigger('click')
    await flushPromises()
    expect(forumApi.updateForumSettings).toHaveBeenCalledWith('c1', { require_tag: false, guidelines: 'Rules' })
    expect(useToastStore().toasts.map(x => x.text)).toContain('Forum rules saved')
    expect(w.emitted('saved')).toHaveLength(1)
    expect(w.get('[data-testid="forum-settings-save"]').attributes('disabled')).toBeDefined()
  })

  it('does not count surrounding whitespace as a change', async () => {
    const w = mountForm({ require_tag: false, guidelines: 'Rules' })
    await w.get('#forum-guidelines').setValue('  Rules ')
    expect(w.get('[data-testid="forum-settings-save"]').attributes('disabled')).toBeDefined()
  })

  it('shows the unsaved bar and Reset while dirty', async () => {
    const w = mountForm()
    await w.get('#forum-guidelines').setValue('x')
    expect(w.text()).toContain('You have unsaved changes')
    await w.get('[data-testid="unsaved-reset"]').trigger('click')
    expect((w.get('#forum-guidelines').element as HTMLTextAreaElement).value).toBe('')
    expect(pendingConfirm.value).toBeNull()
  })
})
