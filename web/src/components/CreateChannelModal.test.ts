import type { VueWrapper } from '@vue/test-utils'

import { channelFixture, categoryFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import CreateChannelModal from './CreateChannelModal.vue'
import BaseDialog from './BaseDialog.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve([])),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

let chat: ReturnType<typeof useChatStore>
let wrapper: VueWrapper

function mountModal(props = {}) {
  wrapper = mount(CreateChannelModal, { props, attachTo: document.body })
  return wrapper
}

const typeButton = (w: VueWrapper, label: string) => w.findAll('button').find(b => b.text().startsWith(label))!
const submitButton = (w: VueWrapper) => w.find('button[type="submit"]')!

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  chat = useChatStore()
  chat.categories = [categoryFixture({ id: "00000000-0000-4000-8000-0000000003ea", name: 'Games' }), categoryFixture({ id: "00000000-0000-4000-8000-0000000003eb", name: 'Work' })]
  chat.createChannel = vi.fn<ReturnType<typeof useChatStore>['createChannel']>(async c => channelFixture({ created_at: '2026-01-01T00:00:00Z', sort_order: 0,  id: "00000000-0000-4000-8000-0000000003ec", name: c.name, type: c.type ?? 'text', topic: c.topic ?? '', category_id: c.categoryId ?? null }))
  chat.createCategory = vi.fn<ReturnType<typeof useChatStore>['createCategory']>(async name => categoryFixture({ id: "00000000-0000-4000-8000-0000000003ed", name }))
})

afterEach(() => {
  wrapper?.unmount()
})

describe('CreateChannelModal', () => {
  it('slugifies text channel names and creates the channel', async () => {
    const w = mountModal({ initialCategoryId: "00000000-0000-4000-8000-0000000003eb" })
    expect(submitButton(w).attributes('disabled')).toBeDefined()
    await w.find<HTMLInputElement>('#channel-name')!.setValue('Off Topic! Talk_1')
    expect(w.find<HTMLInputElement>('#channel-name')!.element.value).toBe('off-topic-talk_1')
    await w.find<HTMLInputElement>('#channel-topic')!.setValue('  anything goes  ')
    expect(w.find<HTMLInputElement>('#channel-category')!.element.value).toBe("00000000-0000-4000-8000-0000000003eb")
    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(chat.createCategory).not.toHaveBeenCalled()
    expect(chat.createChannel).toHaveBeenCalledWith({ categoryId: "00000000-0000-4000-8000-0000000003eb", name: 'off-topic-talk_1', type: 'text', topic: 'anything goes' })
    expect(w.emitted('created')![0]![0]!).toMatchObject({ id: "00000000-0000-4000-8000-0000000003ec" })
    expect(w.emitted('close')!).toHaveLength(1)
  })

  it('keeps voice channel names as typed', async () => {
    const w = mountModal({ initialType: 'voice' })
    expect(typeButton(w, 'Voice channel').attributes('aria-pressed')).toBe('true')
    await w.find<HTMLInputElement>('#channel-name')!.setValue('Lounge & Bar')
    expect(w.find<HTMLInputElement>('#channel-name')!.element.value).toBe('Lounge & Bar')
    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(chat.createChannel).toHaveBeenCalledWith({ categoryId: null, name: 'Lounge & Bar', type: 'voice', topic: '' })
  })

  it('switches the channel type', async () => {
    const w = mountModal()
    expect(typeButton(w, 'Channel').attributes('aria-pressed')).toBe('true')
    await typeButton(w, 'Voice channel').trigger('click')
    expect(typeButton(w, 'Voice channel').attributes('aria-pressed')).toBe('true')
    expect(w.text()).toContain('Any title for the Talk.')
  })

  it("creates a 00000000-0000-4000-8000-0000000003f1 category inline and places the channel in it", async () => {
    const w = mountModal()
    expect(w.findAll('#channel-category option').map(o => o.text())).toEqual(['No category', 'Games', 'Work'])
    await typeButton(w, '+ New category').trigger('click')
    await w.find<HTMLInputElement>('#channel-category')!.setValue('  Projects ')
    await w.find<HTMLInputElement>('#channel-name')!.setValue('roadmap')
    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(chat.createCategory).toHaveBeenCalledWith('Projects', 2)
    expect(vi.mocked(chat.createChannel).mock.calls[0]![0]!.categoryId).toBe("00000000-0000-4000-8000-0000000003ed")
  })

  it('ignores an empty inline category', async () => {
    const w = mountModal()
    await typeButton(w, '+ New category').trigger('click')
    await w.find<HTMLInputElement>('#channel-name')!.setValue('roadmap')
    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(chat.createCategory).not.toHaveBeenCalled()
    expect(vi.mocked(chat.createChannel).mock.calls[0]![0]!.categoryId).toBeNull()
  })

  it('refuses a name that slugifies to nothing', async () => {
    const w = mountModal()
    await w.find<HTMLInputElement>('#channel-name')!.setValue('!!!')
    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(chat.createChannel).not.toHaveBeenCalled()
    expect(w.find('[role="alert"]')!.text()).toContain('Please enter a name.')
  })

  it('shows the server error and stays open', async () => {
    chat.createChannel = vi.fn().mockRejectedValueOnce(new Error('a channel with this name already exists')).mockRejectedValueOnce(new Error(''))
    const w = mountModal()
    await w.find<HTMLInputElement>('#channel-name')!.setValue('general')
    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(w.find('[role="alert"]')!.text()).toContain('a channel with this name already exists')
    expect(w.emitted('close')!).toBeUndefined()
    expect(submitButton(w).text()).toBe('Create channel')

    await w.find('form')!.trigger('submit')
    await flushPromises()
    expect(w.find('[role="alert"]')!.text()).toContain("Couldn't create the channel.")
  })

  it('closes on cancel', async () => {
    const w = mountModal()
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(w.emitted('close')!).toHaveLength(1)
  })
})

it('switches back to existing categories and text type, and forwards shell close', async () => {
  const w = mountModal({ initialType: 'voice' })
  await typeButton(w, 'Channel').trigger('click')
  await typeButton(w, '+ New category').trigger('click')
  await typeButton(w, 'Choose existing').trigger('click')
  expect(w.get('#channel-category').element.tagName).toBe('SELECT')
  await w.get('#channel-category').setValue(chat.categories[0]!.id)
  w.getComponent(BaseDialog).vm.$emit('close')
  expect(w.emitted('close')).toHaveLength(1)
})
