import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import TalkControlBar from './TalkControlBar.vue'
import TalkReactButton from './TalkReactButton.vue'
import TalkReactionFloat from './TalkReactionFloat.vue'
import CreateChannelModal from './CreateChannelModal.vue'
import { buildChannelItems } from '../composables/useNavMenus'
import { createReactionLimiter, splitStage } from '../lib/stage'
import { api } from '../lib/api'
import { PERMISSION_BITS } from '../lib/permissions'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useVoiceStore } from '../stores/voice'
import { setLocale } from '../i18n'
import { channelFixture, userFixture, voiceUserFixture } from '../test-fixtures.fixture'

vi.mock('../lib/api', async original => ({ ...(await original<typeof import('../lib/api')>()), api: vi.fn() }))
vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn(), applyAudioSettings: vi.fn(), toggleCamera: vi.fn() })
}))

const room = 'room-1'
const sent = vi.fn()
let wrapper: ReturnType<typeof mount> | undefined

/** A member of a Stage with the given channel permissions, seated in the Talk. */
function inStage(bits: number, seat: Partial<ReturnType<typeof voiceUserFixture>> = {}) {
  const auth = useAuthStore()
  auth.user = userFixture({ id: 'me' })
  usePermissionsStore().info = {
    owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 1, catalog: [],
    channels: [{ channel_id: room, permissions: bits | PERMISSION_BITS.view_channel }]
  }
  const chat = useChatStore()
  chat.uncategorized = [channelFixture({ id: room, type: 'voice', is_stage: true })]
  chat.sendWSEvent = sent as typeof chat.sendWSEvent
  useServerFeaturesStore().list = ['talks.stage', 'talks.reactions']
  const voice = useVoiceStore()
  voice.setVoiceSnapshot({ [room]: { me: voiceUserFixture({ id: 'me', ...seat }) } })
  voice.setChannel(room)
  voice.isConnected = true
  return voice
}

const SPEAKING = PERMISSION_BITS.connect | PERMISSION_BITS.speak | PERMISSION_BITS.video | PERMISSION_BITS.add_reactions

beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); setLocale('en'); sent.mockClear(); vi.mocked(api).mockReset() })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

describe('the call controls on a Stage', () => {
  it('keep the audience off the microphone, camera and screen and say why', async () => {
    const voice = inStage(SPEAKING)
    wrapper = mount(TalkControlBar)
    await nextTick()
    expect(wrapper.get('[data-testid="talk-mute"]').attributes('disabled')).toBeDefined()
    const camera = wrapper.findAll('button').filter(b => b.attributes('disabled') !== undefined)
    expect(camera.length).toBeGreaterThanOrEqual(3)
    // Whoever joins without talk power starts muted.
    expect(voice.isMuted).toBe(true)
  })

  it('give a member with talk power their microphone back, still muted until they choose', async () => {
    const voice = inStage(SPEAKING)
    wrapper = mount(TalkControlBar)
    voice.handleStageTalkPower({ channel_id: room, user_id: 'me', granted: true })
    await nextTick()
    expect(wrapper.get('[data-testid="talk-mute"]').attributes('disabled')).toBeUndefined()
    expect(voice.isMuted).toBe(true)
    await wrapper.get('[data-testid="talk-mute"]').trigger('click')
    expect(voice.isMuted).toBe(false)
  })

  it('treat a holder of Manage stage as a speaker', async () => {
    const voice = inStage(SPEAKING | PERMISSION_BITS.manage_stage)
    wrapper = mount(TalkControlBar)
    await nextTick()
    expect(wrapper.get('[data-testid="talk-mute"]').attributes('disabled')).toBeUndefined()
    expect(voice.isMuted).toBe(false)
  })

  it('leave an ordinary Talk alone', async () => {
    const voice = inStage(SPEAKING)
    useChatStore().uncategorized = [channelFixture({ id: room, type: 'voice' })]
    wrapper = mount(TalkControlBar)
    await nextTick()
    expect(wrapper.get('[data-testid="talk-mute"]').attributes('disabled')).toBeUndefined()
    expect(voice.isMuted).toBe(false)
  })
})

describe('live reactions', () => {
  it('are sent from the palette, with every emoji the picker offers', async () => {
    inStage(SPEAKING)
    wrapper = mount(TalkReactButton, { props: { buttonClass: 'x' } })
    await wrapper.get('[data-testid="talk-react"]').trigger('click')
    const party = wrapper.findAll('[data-testid="reaction-palette"] button').find(b => b.text() === '🎉')!
    await party.trigger('click')
    expect(sent).toHaveBeenCalledWith('talk_reaction', { emoji: '🎉' })
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([true])
    await wrapper.get('[data-testid="talk-react"]').trigger('click')
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })

  it('close with Escape or when the palette asks to', async () => {
    inStage(SPEAKING)
    wrapper = mount(TalkReactButton)
    await wrapper.get('[data-testid="talk-react"]').trigger('click')
    expect(wrapper.find('[data-testid="reaction-palette"]').exists()).toBe(true)
    await wrapper.get('div').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="reaction-palette"]').exists()).toBe(false)
    await wrapper.get('[data-testid="talk-react"]').trigger('click')
    wrapper.findComponent({ name: 'ReactionPalette' }).vm.$emit('close')
    await nextTick()
    expect(wrapper.find('[data-testid="reaction-palette"]').exists()).toBe(false)
  })

  it('stop at the allowance the server enforces, so nothing dropped is sent', () => {
    let now = 1000
    const allow = createReactionLimiter(() => now)
    expect([1, 2, 3, 4, 5, 6].map(() => allow())).toEqual([true, true, true, true, true, false])
    now += 10_001
    expect(allow()).toBe(true)
  })

  it('float over the tile as decoration and stay still without motion', () => {
    wrapper = mount(TalkReactionFloat, { props: { reactions: [{ id: 1, emoji: '🎉' }, { id: 2, emoji: '👍' }] } })
    const overlay = wrapper.get('[data-testid="talk-reactions"]')
    expect(overlay.attributes('aria-hidden')).toBe('true')
    expect(wrapper.findAll('[data-testid="talk-reaction"]').map(r => r.text())).toEqual(['🎉', '👍'])
    expect(overlay.attributes('style')).toBeUndefined()
    wrapper.unmount()
    wrapper = mount(TalkReactionFloat)
    expect(wrapper.find('[data-testid="talk-reactions"]').exists()).toBe(false)
  })
})

describe('Make Stage in the Talk menu', () => {
  const talk = channelFixture({ id: room, type: 'voice' })
  const ids = (c = talk) => buildChannelItems(c).map(i => i.id)

  function asManager() {
    useAuthStore().user = userFixture({ id: 'me', role: 'admin' })
    useServerFeaturesStore().list = ['talks.stage']
  }

  it('turns a Talk into a Stage and back', async () => {
    asManager()
    vi.mocked(api).mockResolvedValue(channelFixture({ id: room, type: 'voice', is_stage: true }))
    const make = buildChannelItems(talk).find(i => i.id === 'stage-toggle')!
    expect(make.label).toBe('Make Stage')
    await make.action?.()
    expect(api).toHaveBeenCalledWith(`/api/admin/channels/${room}`, expect.objectContaining({ method: 'PATCH', json: { is_stage: true } }))
    const back = buildChannelItems(channelFixture({ id: room, type: 'voice', is_stage: true })).find(i => i.id === 'stage-toggle')!
    expect(back.label).toBe('Make normal Talk')
    await back.action?.()
    expect(api).toHaveBeenLastCalledWith(`/api/admin/channels/${room}`, expect.objectContaining({ json: { is_stage: false } }))
  })

  it('is offered for permanent Talks only, to channel managers, on servers with Stages', () => {
    asManager()
    expect(ids(channelFixture({ id: room, type: 'text' }))).not.toContain('stage-toggle')
    expect(ids(channelFixture({ id: room, type: 'voice', is_lobby: true }))).not.toContain('stage-toggle')
    expect(ids(channelFixture({ id: room, type: 'voice', is_temp: true }))).not.toContain('stage-toggle')
    useServerFeaturesStore().list = []
    expect(ids()).not.toContain('stage-toggle')
    useAuthStore().user = userFixture({ id: 'me', role: 'user' })
    useServerFeaturesStore().list = ['talks.stage']
    expect(ids()).not.toContain('stage-toggle')
  })
})

describe('creating a Talk as a Stage', () => {
  function mountModal(initialType: 'text' | 'voice') {
    useServerFeaturesStore().list = ['talks.stage']
    const chat = useChatStore()
    chat.createChannel = vi.fn(async c => channelFixture({ id: 'new', name: c.name, type: c.type ?? 'text' })) as typeof chat.createChannel
    wrapper = mount(CreateChannelModal, { props: { initialType }, attachTo: document.body })
    return chat
  }

  it('offers the choice for Talks and sends it', async () => {
    const chat = mountModal('voice')
    await wrapper!.get('#channel-name').setValue('Town hall')
    await wrapper!.get('[data-testid="create-stage"]').setValue(true)
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(chat.createChannel).toHaveBeenCalledWith(expect.objectContaining({ name: 'Town hall', type: 'voice', isStage: true }))
  })

  it('does not offer it for text channels', () => {
    mountModal('text')
    expect(wrapper!.find('[data-testid="create-stage"]').exists()).toBe(false)
  })
})

describe('arranging a Stage', () => {
  it('lists speakers, the audience and the hands waiting longest first', () => {
    const split = splitStage([
      { id: 'a', talk_power: true },
      { id: 'b', hand_raised_at: '2026-01-01T10:05:00Z' },
      { id: 'c', hand_raised_at: '2026-01-01T10:01:00Z' },
      { id: 'd' }
    ])
    expect(split.speakers.map(u => u.id)).toEqual(['a'])
    expect(split.audience.map(u => u.id)).toEqual(['b', 'c', 'd'])
    expect(split.hands.map(u => u.id)).toEqual(['c', 'b'])
  })
})
