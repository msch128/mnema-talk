import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import CreateChannelModal from './CreateChannelModal.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'

vi.mock('../lib/api', () => ({
  api: vi.fn(() => Promise.resolve([])),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

let chat
let wrapper

function mountModal(props = {}) {
  wrapper = mount(CreateChannelModal, { props, attachTo: document.body })
  return wrapper
}

const typeButton = (w, label) => w.findAll('button').find(b => b.text().startsWith(label))
const submitButton = w => w.find('button[type="submit"]')

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  chat = useChatStore()
  chat.categories = [{ id: 'cat-1', name: 'Games' }, { id: 'cat-2', name: 'Work' }]
  chat.createChannel = vi.fn(async c => ({ id: 'new-channel', ...c }))
  chat.createCategory = vi.fn(async name => ({ id: 'new-cat', name }))
})

afterEach(() => {
  wrapper?.unmount()
})

describe('CreateChannelModal', () => {
  it('slugifies text channel names and creates the channel', async () => {
    const w = mountModal({ initialCategoryId: 'cat-2' })
    expect(submitButton(w).attributes('disabled')).toBeDefined()
    await w.find('#channel-name').setValue('Off Topic! Talk_1')
    expect(w.find('#channel-name').element.value).toBe('off-topic-talk_1')
    await w.find('#channel-topic').setValue('  anything goes  ')
    expect(w.find('#channel-category').element.value).toBe('cat-2')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(chat.createCategory).not.toHaveBeenCalled()
    expect(chat.createChannel).toHaveBeenCalledWith({ categoryId: 'cat-2', name: 'off-topic-talk_1', type: 'text', topic: 'anything goes' })
    expect(w.emitted('created')[0][0]).toMatchObject({ id: 'new-channel' })
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('keeps voice channel names as typed', async () => {
    const w = mountModal({ initialType: 'voice' })
    expect(typeButton(w, 'Voice channel').attributes('aria-pressed')).toBe('true')
    await w.find('#channel-name').setValue('Lounge & Bar')
    expect(w.find('#channel-name').element.value).toBe('Lounge & Bar')
    await w.find('form').trigger('submit')
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

  it('creates a new category inline and places the channel in it', async () => {
    const w = mountModal()
    expect(w.findAll('#channel-category option').map(o => o.text())).toEqual(['No category', 'Games', 'Work'])
    await typeButton(w, '+ New category').trigger('click')
    await w.find('#channel-category').setValue('  Projects ')
    await w.find('#channel-name').setValue('roadmap')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(chat.createCategory).toHaveBeenCalledWith('Projects', 2)
    expect(chat.createChannel.mock.calls[0][0].categoryId).toBe('new-cat')
  })

  it('ignores an empty inline category', async () => {
    const w = mountModal()
    await typeButton(w, '+ New category').trigger('click')
    await w.find('#channel-name').setValue('roadmap')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(chat.createCategory).not.toHaveBeenCalled()
    expect(chat.createChannel.mock.calls[0][0].categoryId).toBeNull()
  })

  it('refuses a name that slugifies to nothing', async () => {
    const w = mountModal()
    await w.find('#channel-name').setValue('!!!')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(chat.createChannel).not.toHaveBeenCalled()
    expect(w.find('[role="alert"]').text()).toContain('Please enter a name.')
  })

  it('shows the server error and stays open', async () => {
    chat.createChannel = vi.fn().mockRejectedValueOnce(new Error('a channel with this name already exists')).mockRejectedValueOnce(new Error(''))
    const w = mountModal()
    await w.find('#channel-name').setValue('general')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[role="alert"]').text()).toContain('a channel with this name already exists')
    expect(w.emitted('close')).toBeUndefined()
    expect(submitButton(w).text()).toBe('Create channel')

    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[role="alert"]').text()).toContain("Couldn't create the channel.")
  })

  it('closes on cancel', async () => {
    const w = mountModal()
    await w.findAll('button').find(b => b.text() === 'Cancel').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })
})
