import { messageFixture, userFixture, categoryFixture, channelFixture, onboardingStateFixture, requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ChatArea from './ChatArea.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useOnboardingStore } from '../stores/onboarding'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { setLocale, i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { stageAttachment, unstageAttachment } from '../lib/attachmentsApi'
import { pendingConfirm } from '../lib/confirm'
import { viewerState, closeViewer } from '../composables/useMediaViewer'

vi.mock('../lib/attachmentsApi', () => ({ stageAttachment: vi.fn(), unstageAttachment: vi.fn(async () => undefined) }))

let w!: ReturnType<typeof mount<typeof ChatArea>>

const staged = (id: string, name = 'a.png') => ({ id, original_filename: name, mime_type: 'image/png', size_bytes: 1, url: `/api/media/${id}`, is_deleted: false })
const image = (name: string) => new File(['x'], name, { type: 'image/png' })

function setup() {
  useAuthStore().user = userFixture({ id: 'me', username: 'me', role: 'user' })
  useOnboardingStore().state = onboardingStateFixture({ dismissed_tips: ['search', 'mention', 'talk'] })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: 'c', channels: [channelFixture({ id: 'ch1', name: 'lobby', type: 'text' })] })]
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'lobby', type: 'text' })
  chat.messages = [messageFixture({ id: 'a', channel_id: 'ch1', user_id: 'u2', attachments: [], reactions: [] })]
  const send = vi.spyOn(chat, 'sendMessage').mockResolvedValue(null)
  w = mount(ChatArea, { attachTo: document.body, global: { plugins: [i18nPlugin], directives: { tooltip } } })
  return send
}

const composer = () => w.find<HTMLTextAreaElement>('textarea[data-composer-input]')
const cards = () => w.findAll('[data-testid="attachment-tray"] li')
const key = (init: KeyboardEventInit) => composer().element.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }))

function paste(files: File[]) {
  const e = new Event('paste', { bubbles: true, cancelable: true }) as ClipboardEvent
  Object.defineProperty(e, 'clipboardData', { value: { files, types: ['Files'], getData: () => '' } })
  composer().element.dispatchEvent(e)
  return e
}

function pick(files: File[]) {
  const input = w.find<HTMLInputElement>('input[type="file"]').element
  Object.defineProperty(input, 'files', { configurable: true, value: files })
  input.dispatchEvent(new Event('change'))
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(null, { status: 204 })))
  vi.mocked(stageAttachment).mockReset()
  vi.mocked(unstageAttachment).mockClear()
  let n = 0
  vi.mocked(stageAttachment).mockImplementation(async (_c, file) => staged(`att${++n}`, file.name))
})

afterEach(() => {
  w?.unmount()
  pendingConfirm.value = null
  closeViewer()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ChatArea attachment tray', () => {
  it('stages a pasted image and does not send it', async () => {
    const send = setup()
    const e = paste([image('shot.png')])
    await flushPromises()
    expect(e.defaultPrevented).toBe(true)
    expect(cards()).toHaveLength(1)
    expect(cards()[0]?.text()).toContain('shot.png')
    expect(stageAttachment).toHaveBeenCalledTimes(1)
    expect(send).not.toHaveBeenCalled()
  })

  it('adds pasted, dropped and picked files to the same tray', async () => {
    const send = setup()
    paste([image('one.png')])
    pick([image('two.png')])
    const drop = new Event('drop', { bubbles: true, cancelable: true }) as DragEvent
    Object.defineProperty(drop, 'dataTransfer', { value: { types: ['Files'], files: [image('three.png')] } })
    w.element.dispatchEvent(drop)
    await flushPromises()
    expect(cards().map(c => c.find('p').text())).toEqual(['one.png', 'two.png', 'three.png'])
    expect(send).not.toHaveBeenCalled()
  })

  it('sends one message with the text and all the files in tray order, on Enter', async () => {
    const send = setup()
    paste([image('1.png'), image('2.png'), image('3.png')])
    await flushPromises()
    await composer().setValue('three pictures')
    key({ key: 'Enter' })
    await flushPromises()
    expect(send).toHaveBeenCalledTimes(1)
    expect(send).toHaveBeenCalledWith('three pictures', null, null, ['att1', 'att2', 'att3'])
    expect(cards()).toHaveLength(0)
    expect(composer().element.value).toBe('')
  })

  it('sends files without any text, with the send button', async () => {
    const send = setup()
    paste([image('1.png')])
    await flushPromises()
    const sendButton = requireValue(w.findAll('button').find(b => b.find('svg.lucide-arrow-up').exists()))
    expect(sendButton.attributes('disabled')).toBeUndefined()
    await sendButton.trigger('click')
    await flushPromises()
    expect(send).toHaveBeenCalledWith('', null, null, ['att1'])
  })

  it('keeps Send disabled with neither text nor files', () => {
    setup()
    const sendButton = requireValue(w.findAll('button').find(b => b.find('svg.lucide-arrow-up').exists()))
    expect(sendButton.attributes('disabled')).toBeDefined()
  })

  it('holds back a send while a file failed and keeps the text', async () => {
    const send = setup()
    vi.mocked(stageAttachment).mockRejectedValue(new Error('x'))
    paste([image('bad.png')])
    await flushPromises()
    await composer().setValue('hello')
    key({ key: 'Enter' })
    await flushPromises()
    expect(send).not.toHaveBeenCalled()
    expect(composer().element.value).toBe('hello')
    expect(w.find('[data-attachment-status="error"]').exists()).toBe(true)
    expect(useToastStore().toasts.at(-1)?.text).toBe('Some files could not be uploaded. Retry or remove them to send.')
  })

  it('keeps the files when the send fails', async () => {
    const send = setup()
    send.mockRejectedValueOnce(new Error('server said no'))
    paste([image('1.png')])
    await flushPromises()
    await composer().setValue('hi')
    key({ key: 'Enter' })
    await flushPromises()
    expect(cards()).toHaveLength(1)
    expect(composer().element.value).toBe('hi')
  })

  it('keeps a file whose upload failed, with its error and a retry', async () => {
    setup()
    vi.mocked(stageAttachment).mockRejectedValueOnce(new Error('too big')).mockResolvedValueOnce(staged('att9'))
    pick([image('a.png')])
    await flushPromises()
    await w.find('[data-attachment-status="error"] button').trigger('click')
    await flushPromises()
    expect(w.find('[data-attachment-status="ready"]').exists()).toBe(true)
    expect(stageAttachment).toHaveBeenCalledTimes(2)
  })

  it('takes ten files and turns the eleventh away', async () => {
    setup()
    paste(Array.from({ length: 11 }, (_, i) => image(`${i}.png`)))
    await flushPromises()
    expect(cards()).toHaveLength(10)
    expect(useToastStore().toasts.some(x => x.text === 'A message carries at most 10 files.')).toBe(true)
  })

  it('removes one file from the tray and from the server', async () => {
    setup()
    paste([image('1.png'), image('2.png')])
    await flushPromises()
    await w.find('[aria-label="Remove 1.png"]').trigger('click')
    expect(cards().map(c => c.find('p').text())).toEqual(['2.png'])
    expect(unstageAttachment).toHaveBeenCalledWith('ch1', 'att1')
  })

  it('Escape in an empty composer asks, then clears the tray', async () => {
    setup()
    paste([image('1.png')])
    await flushPromises()
    key({ key: 'Escape' })
    expect(pendingConfirm.value).not.toBeNull()
    requireValue(pendingConfirm.value).resolve(true)
    await flushPromises()
    expect(cards()).toHaveLength(0)
    expect(unstageAttachment).toHaveBeenCalledWith('ch1', 'att1')
  })

  it('opens a picture of the tray in the viewer', async () => {
    setup()
    paste([image('1.png'), image('2.png')])
    await flushPromises()
    await w.find('[aria-label="View 2.png"]').trigger('click')
    expect(viewerState.open).toBe(true)
    expect(viewerState.index).toBe(1)
    expect(viewerState.items.map(i => i.name)).toEqual(['1.png', '2.png'])
  })

  it('refuses files where the member may not attach', async () => {
    // No attach right: the composer offers no button and a paste says why.
    vi.spyOn(usePermissionsStore(), 'canIn').mockImplementation((_channel, right) => right !== 'attach_files')
    setup()
    paste([image('x.png')])
    await flushPromises()
    expect(cards()).toHaveLength(0)
    expect(useToastStore().toasts.at(-1)?.text).toBe('You can’t attach files here.')
    expect(stageAttachment).not.toHaveBeenCalled()
  })
})
