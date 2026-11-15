import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LifetimeMark from './LifetimeMark.vue'
import ChatComposer from './ChatComposer.vue'
import MessageAttachments from './MessageAttachments.vue'
import ReplyPreview from './ReplyPreview.vue'
import ChannelAutoDeleteField from '../../admin/components/ChannelAutoDeleteField.vue'
import { useChatStore } from '../../stores/chat'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { lifetimeLabel, remainingLabel } from '../../shared/lib/lifetime'
import { countsText, periodLabel } from '../../shared/lib/autoDelete'
import { closeViewer, openViewer, viewerState } from '../../shared/composables/useMediaViewer'
import { i18nPlugin, setLocale } from '../../i18n'
import { channelFixture, fixtureId } from '../../test-fixtures.fixture'
import type { MediaAttachment } from '../../types/domain'

// The web review of the timed messages (2026-11): what a reader and a screen
// reader see of a lifetime, a removed file and the settings.

enableAutoUnmount(afterEach)

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  localStorage.clear()
})
afterEach(() => vi.unstubAllGlobals())

const tooltip = { directives: { tooltip: () => undefined } }

describe('the remaining-time mark', () => {
  it('is named by visible and screen-reader text, not by an aria-label on a span', () => {
    const w = mount(LifetimeMark, { props: { expiresAt: new Date(Date.now() + 3 * 3600_000 - 1000).toISOString() }, global: tooltip })
    expect(w.attributes('aria-label')).toBeUndefined()
    expect(w.find('[aria-hidden="true"]:not(svg)').exists()).toBe(false)
    expect(w.text()).toBe('Disappears in 3 h')
    expect(w.find('[aria-live]').exists()).toBe(false)
  })
})

describe('units of time', () => {
  it('speak the singular for one, in both languages', () => {
    expect([lifetimeLabel(86400), lifetimeLabel(7 * 86400), periodLabel(1), periodLabel(24), periodLabel(168)]).toEqual(['1 day', '1 week', '1 hour', '1 day', '1 week'])
    setLocale('de')
    expect([lifetimeLabel(86400), periodLabel(24), periodLabel(168)]).toEqual(['1 Tag', '1 Tag', '1 Woche'])
  })

  it('write a lifetime of hours and minutes as such, and round the time left to its unit', () => {
    expect(lifetimeLabel(75 * 3600 + 600)).toBe('75 h 10 min')
    expect(remainingLabel(65_000)).toBe('65 s')
    expect(remainingLabel(61 * 60_000)).toBe('61 min')
    expect(remainingLabel(25 * 3600_000)).toBe('25 h')
    setLocale('de')
    expect([lifetimeLabel(3600), lifetimeLabel(300), remainingLabel(3 * 86400_000)]).toEqual(['1 Std', '5 Min', '3 Tage'])
  })

  it('name the counts of a dry run in words, leaving out a zero', () => {
    expect(countsText({ messages: 450, attachments: 0 })).toBe('450 messages')
    expect(countsText({ messages: 1, attachments: 1 })).toBe('1 message and 1 file')
    setLocale('de')
    expect(countsText({ messages: 0, attachments: 3 })).toBe('3 Dateien')
  })
})

describe('the composer timer', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ message_lifetimes_enabled: true, message_lifetime_max_seconds: 2592000 }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    useServerFeaturesStore().list = ['messages.lifetime']
    useChatStore().activeChannel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
  })

  const mountComposer = () => mount(ChatComposer, { props: { modelValue: 'x', canSend: true }, global: tooltip, attachTo: document.body })

  it('shows the whole help line, wrapping instead of cutting it, and keeps the menu to choices', async () => {
    const w = mountComposer()
    await flushPromises()
    await w.get('[data-testid="lifetime-button"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[data-menu-info]')).toBeNull()
    const item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => el.textContent?.trim() === '1 h')
    item!.click()
    await flushPromises()
    const help = w.get('[data-testid="lifetime-help"]')
    expect(help.classes()).not.toContain('truncate')
    expect(help.text()).toBe('Disappears for everyone after 1 h. Copies, screenshots and notifications can keep it.')
  })

  it('gives the focus back to the timer button after a custom lifetime', async () => {
    const w = mountComposer()
    await flushPromises()
    await w.get('[data-testid="lifetime-button"]').trigger('click')
    await flushPromises()
    ;[...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => el.textContent?.trim() === 'Custom…')!.click()
    await flushPromises()
    document.body.querySelector<HTMLButtonElement>('[data-testid="lifetime-custom"] button[type="submit"]')!.click()
    await flushPromises()
    expect(document.activeElement).toBe(w.get('[data-testid="lifetime-button"]').element)
  })
})

const removed = (over: Partial<MediaAttachment>): MediaAttachment => ({
  id: fixtureId(40), url: `/api/media/${fixtureId(40)}`, original_filename: 'plan.pdf', mime_type: 'application/pdf', size_bytes: 2048, is_deleted: true, ...over
})

describe('a removed file', () => {
  it('keeps a picture tile within the column, its height from the ratio', () => {
    const w = mount(MessageAttachments, { props: { attachments: [removed({ original_filename: 'big.png', mime_type: 'image/png', width: 800, height: 600 })] } })
    const style = w.get('[data-media-placeholder="gone"]').attributes('style') ?? ''
    expect(style).toContain('max-width: 100%')
    expect(style).toContain('aspect-ratio: 800 / 600')
    expect(style).not.toMatch(/(^|;)\s*height:\s*\d/)
    expect(w.get('[data-attachment="image"]').classes().join(' ')).toContain('max-w-[min(100%,28rem)]')
  })

  it('keeps a file row at its size with its name, marked as removed', () => {
    const w = mount(MessageAttachments, { props: { attachments: [removed({})] } })
    expect(w.find('[data-media-placeholder="gone"]').exists()).toBe(false)
    const row = w.get('[data-attachment="file"]')
    expect(row.text()).toContain('plan.pdf')
    expect(row.text()).toContain('File removed')
    expect(row.find('a[href]').exists()).toBe(false)
  })

  it('shows as removed in the reply preview of a message that only had files', () => {
    const w = mount(ReplyPreview, {
      props: { reply: { id: fixtureId(7), user_id: fixtureId(2), username: 'alice', display_name: 'Alice', content: '', has_attachments: true, attachments_removed: true, deleted: false } },
      global: { plugins: [i18nPlugin], ...tooltip }
    })
    expect(w.text()).toContain('File removed')
    expect(w.text()).not.toContain('attachment')
  })
})

describe('the image viewer', () => {
  it('closes when the message of its picture ends or is deleted', async () => {
    openViewer({ items: [{ src: '/api/media/x', name: 'x.png' }], index: 0, message: { id: fixtureId(8), channelId: fixtureId(5), senderName: 'Alice', sentAt: '2026-11-13T10:00:00Z' } })
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'message_delete', payload: { id: fixtureId(8), channel_id: fixtureId(5), parent_id: null } })
    await flushPromises()
    expect(viewerState.open).toBe(false)
    closeViewer()
  })
})

describe('the channel auto-delete field', () => {
  it('says only what the chosen scope does', () => {
    useServerFeaturesStore().list = ['channels.auto_delete', 'channels.auto_delete.v2']
    const w = mount(ChannelAutoDeleteField, { props: { modelValue: '48:attachments' }, global: { plugins: [i18nPlugin] } })
    expect(w.text()).not.toContain('Messages older than this are deleted')
    expect(w.text()).toContain('The messages stay.')
  })

  it('keeps the unit that was chosen', async () => {
    useServerFeaturesStore().list = ['channels.auto_delete', 'channels.auto_delete.v2']
    const w = mount(ChannelAutoDeleteField, { props: { modelValue: '5:all', 'onUpdate:modelValue': (v: string) => w.setProps({ modelValue: v }) }, global: { plugins: [i18nPlugin] } })
    await w.get('#auto-delete-amount').setValue('24')
    await w.get('#auto-delete-amount').trigger('change')
    await flushPromises()
    expect((w.get('#auto-delete-unit').element as HTMLSelectElement).value).toBe('hours')
    expect((w.get('#auto-delete-amount').element as HTMLInputElement).value).toBe('24')
  })
})
