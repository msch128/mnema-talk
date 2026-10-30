import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import TalkOverviewDialog from './TalkOverviewDialog.vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useVoiceStore } from '../stores/voice'
import { i18nPlugin, setLocale } from '../i18n'
import { categoryFixture, channelFixture, userFixture, voiceUserFixture } from '../test-fixtures.fixture'

const joinVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel }) }))
const confirm = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (o: unknown) => confirm(o) }))
vi.mock('../lib/router', () => ({ navigate: vi.fn() }))

let wrapper: VueWrapper
const talk = channelFixture({ type: 'voice', name: 'Lounge' })
const ada = userFixture({ id: '00000000-0000-4000-8000-0000000000a1', username: 'ada', display_name: 'Ada' })
const bo = userFixture({ id: '00000000-0000-4000-8000-0000000000b2', username: 'bo', display_name: 'Bo' })

function open(channelId = talk.id, perms: 'connect' | 'none' = 'connect') {
  const chat = useChatStore()
  chat.categories = [categoryFixture({ channels: [talk] })]
  chat.selectChannel = vi.fn() as unknown as typeof chat.selectChannel
  const voice = useVoiceStore()
  voice.channelUsers = { [talk.id]: { [ada.id]: voiceUserFixture({ ...ada }), [bo.id]: voiceUserFixture({ ...bo, muted: true }) } }
  vi.spyOn(usePermissionsStore(), 'canIn').mockReturnValue(perms === 'connect')
  wrapper = mount(TalkOverviewDialog, { attachTo: document.body, props: { channelId }, global: { plugins: [i18nPlugin] } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  joinVoiceChannel.mockReset()
  confirm.mockReset().mockResolvedValue(false)
})
afterEach(() => {
  wrapper.unmount()
  document.body.innerHTML = ''
})

describe('the Talk overview of a profile', () => {
  it.each([['en', '2 participants', 'Join'], ['de', '2 Teilnehmer', 'Beitreten']])('lists who is in the Talk and offers Join (%s)', (lang, count, join) => {
    setLocale(lang as 'en' | 'de')
    open()
    expect(wrapper.text()).toContain('Lounge')
    expect(wrapper.text()).toContain(count)
    expect(wrapper.findAll('[data-participant]').map(r => r.attributes('data-participant'))).toEqual(expect.arrayContaining(['ada', 'bo']))
    expect(wrapper.find('[data-mute-marks]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="talk-overview-join"]').text()).toBe(join)
  })

  it('joins the Talk and closes', async () => {
    setLocale('en')
    open()
    await wrapper.get('[data-testid="talk-overview-join"]').trigger('click')
    await flushPromises()
    expect(joinVoiceChannel).toHaveBeenCalledWith(talk.id)
    expect(wrapper.emitted('joined')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('asks before leaving the Talk the viewer is in', async () => {
    setLocale('en')
    open()
    const voice = useVoiceStore()
    voice.warnSwitchChannel = true
    voice.currentChannelId = 'other'
    await wrapper.get('[data-testid="talk-overview-join"]').trigger('click')
    await flushPromises()
    expect(joinVoiceChannel).not.toHaveBeenCalled()
    expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ title: 'Switch Talk?' }))
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('shows the people but no Join without the Connect permission', () => {
    setLocale('de')
    open(talk.id, 'none')
    expect(wrapper.find('[data-testid="talk-overview-join"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="talk-overview-cannot"]').text()).toBe('Du kannst diesem Talk nicht beitreten.')
    expect(wrapper.findAll('[data-participant]')).toHaveLength(2)
  })

  it('reveals nothing about a Talk the viewer cannot see', () => {
    setLocale('de')
    open('hidden-talk')
    expect(wrapper.findAll('[data-participant]')).toHaveLength(0)
    expect(wrapper.get('[data-testid="talk-overview-hidden"]').text()).toBe('Dieser Talk ist für dich nicht verfügbar.')
    expect(wrapper.find('[data-testid="talk-overview-join"]').exists()).toBe(false)
  })

  it('selects a participant', async () => {
    setLocale('en')
    open()
    await wrapper.get('[data-participant="ada"]').trigger('click')
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ username: 'ada' })
  })
})
