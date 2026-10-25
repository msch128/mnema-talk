import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import StageLayout from './StageLayout.vue'
import TalkRoster from './TalkRoster.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useVoiceStore } from '../stores/voice'
import { setLocale } from '../i18n'
import { channelFixture, userFixture, voiceUserFixture } from '../test-fixtures.fixture'

const room = 'room-1'
const sent = vi.fn()
let wrapper: ReturnType<typeof mount> | undefined

function world(opts: { owner?: boolean } = {}) {
  const auth = useAuthStore()
  auth.user = userFixture({ id: 'me', role: opts.owner ? 'admin' : 'user' })
  const chat = useChatStore()
  chat.uncategorized = [channelFixture({ id: room, type: 'voice', is_stage: true })]
  chat.sendWSEvent = sent as typeof chat.sendWSEvent
  useServerFeaturesStore().list = ['talks.stage']
  const voice = useVoiceStore()
  const users = {
    me: voiceUserFixture({ id: 'me', username: 'me', display_name: 'Me' }),
    amy: voiceUserFixture({ id: 'amy', username: 'amy', display_name: 'Amy', talk_power: true }),
    bob: voiceUserFixture({ id: 'bob', username: 'bob', display_name: 'Bob', hand_raised_at: '2026-01-01T10:05:00Z' }),
    cat: voiceUserFixture({ id: 'cat', username: 'cat', display_name: 'Cat', hand_raised_at: '2026-01-01T10:01:00Z' }),
    dan: voiceUserFixture({ id: 'dan', username: 'dan', display_name: 'Dan' })
  }
  voice.setVoiceSnapshot({ [room]: users })
  return { voice, users: Object.values(users) }
}

function mountLayout(users: ReturnType<typeof world>['users'], connected = true) {
  wrapper = mount(StageLayout, {
    props: {
      users, channelId: room, connected,
      streamOf: () => null, canFocusCamera: () => false, isCameraFocused: () => false
    },
    global: {
      stubs: {
        TalkTile: {
          props: ['user'],
          emits: ['focus-camera', 'fullscreen-camera', 'watch-stream', 'menu'],
          template: '<div data-testid="tile" @click="$emit(\'focus-camera\'); $emit(\'fullscreen-camera\'); $emit(\'watch-stream\'); $emit(\'menu\', $event)">{{ user.display_name }}</div>'
        }
      }
    }
  })
  return wrapper
}

beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); setLocale('en'); sent.mockClear() })
afterEach(() => { wrapper?.unmount() })

describe('speaker tile size', () => {
  const widths = (speakers: number) => {
    const { users } = world()
    const picked = users.map((u, i) => (i === 0 || i <= speakers - 1 ? { ...u, talk_power: true } : { ...u, talk_power: false }))
    mountLayout(picked.map(u => (u.talk_power ? u : { ...u, hand_raised_at: undefined })))
    return wrapper!.findAll('[data-testid="stage-speakers"] > div').map(d => d.classes().find(c => c.startsWith('w-[')))
  }

  it('grows with the room: one speaker is large, more share the width in smaller tiles', () => {
    const [one] = widths(1)
    expect(one).toContain('30rem')
    wrapper?.unmount()
    const two = widths(2)
    expect(two).toHaveLength(2)
    expect(two[0]).toContain('22.5rem')
    wrapper?.unmount()
    expect(widths(4)[0]).toContain('17.5rem')
  })
})

describe('the Stage layout', () => {
  it('puts members with talk power in the speakers row and the rest in the audience', () => {
    const { users } = world()
    const w = mountLayout(users)
    expect(w.findAll('[data-testid="tile"]').map(t => t.text())).toEqual(['Amy'])
    expect(w.findAll('[data-testid="stage-listener"]').map(t => t.text())).toEqual(expect.arrayContaining([expect.stringContaining('Bob'), expect.stringContaining('Dan')]))
    expect(w.get('[data-testid="stage-audience-count"]').text()).toBe('4')
  })

  it('says so when nobody speaks', () => {
    const { users } = world()
    const w = mountLayout(users.filter(u => u.id !== 'amy'))
    expect(w.find('[data-testid="stage-nobody"]').exists()).toBe(true)
  })

  it('lets the audience raise and lower their hand, but not speakers', async () => {
    const { users, voice } = world()
    const w = mountLayout(users)
    await w.get('[data-testid="stage-raise-hand"]').trigger('click')
    expect(sent).toHaveBeenCalledWith('stage_hand', { raised: true })
    voice.handleStageHand({ channel_id: room, user_id: 'me', raised: true, at: '2026-01-01T10:09:00Z' })
    await nextTick()
    expect(w.get('[data-testid="stage-raise-hand"]').attributes('aria-pressed')).toBe('true')
    await w.get('[data-testid="stage-raise-hand"]').trigger('click')
    expect(sent).toHaveBeenLastCalledWith('stage_hand', { raised: false })
    voice.handleStageTalkPower({ channel_id: room, user_id: 'me', granted: true })
    await nextTick()
    expect(w.find('[data-testid="stage-raise-hand"]').exists()).toBe(false)
  })

  it('shows no queue and no talk power buttons to a member without Manage stage', () => {
    const { users } = world()
    const w = mountLayout(users)
    expect(w.find('[data-testid="stage-hands"]').exists()).toBe(false)
    expect(w.find('[data-testid="stage-give"]').exists()).toBe(false)
    expect(w.find('[data-testid="stage-take"]').exists()).toBe(false)
    // Everybody still sees whose hand is up.
    expect(w.findAll('[data-testid="stage-listener-hand"]')).toHaveLength(2)
  })
})

describe('the tiles and rows of a Stage', () => {
  it('pass the tile choices and the context menu on', async () => {
    const { users } = world()
    const w = mountLayout(users)
    await w.get('[data-testid="tile"]').trigger('click')
    expect(w.emitted('focus-camera')?.[0]).toEqual([expect.objectContaining({ id: 'amy' })])
    expect(w.emitted('fullscreen-camera')?.[0]).toEqual(['amy'])
    expect(w.emitted('watch-stream')?.[0]).toEqual(['amy'])
    expect(w.emitted('menu')).toHaveLength(1)
    await w.get('[data-testid="stage-listener"]').trigger('contextmenu')
    expect(w.emitted('menu')).toHaveLength(2)
  })

  it('name members without a display name by their username and show an empty audience calmly', () => {
    const { users } = world({ owner: true })
    const bare = users.map(u => (u.id === 'bob' ? { ...u, display_name: '' } : u))
    const w = mountLayout(bare)
    expect(w.get('[data-testid="stage-hands"]').text()).toContain('bob')
    wrapper?.unmount()
    const alone = mountLayout(users.filter(u => u.id === 'amy'))
    expect(alone.text()).toContain('Nobody is listening yet.')
  })
})

describe('managing a Stage', () => {
  it('lists raised hands oldest first and accepts with one click', async () => {
    const { users } = world({ owner: true })
    const w = mountLayout(users)
    const queue = w.findAll('[data-testid="stage-hand"]').map(r => r.text())
    expect(queue).toHaveLength(2)
    expect(queue[0]).toContain('Cat')
    expect(queue[1]).toContain('Bob')
    expect(w.get('[data-testid="stage-hands-count"]').text()).toBe('2')
    await w.findAll('[data-testid="stage-accept"]')[0]!.trigger('click')
    expect(sent).toHaveBeenCalledWith('stage_talk_power', { user_id: 'cat', granted: true })
  })

  it('dismisses a hand, gives and takes talk power, and has no raise hand button', async () => {
    const { users } = world({ owner: true })
    const w = mountLayout(users)
    await w.findAll('[data-testid="stage-dismiss"]')[1]!.trigger('click')
    expect(sent).toHaveBeenCalledWith('stage_hand', { user_id: 'bob', raised: false })
    await w.findAll('[data-testid="stage-give"]')[0]!.trigger('click')
    expect(sent).toHaveBeenLastCalledWith('stage_talk_power', expect.objectContaining({ granted: true }))
    await w.get('[data-testid="stage-take"]').trigger('click')
    expect(sent).toHaveBeenLastCalledWith('stage_talk_power', { user_id: 'amy', granted: false })
    expect(w.find('[data-testid="stage-raise-hand"]').exists()).toBe(false)
  })

  it('shows an empty queue calmly', () => {
    const { users } = world({ owner: true })
    const w = mountLayout(users.filter(u => !u.hand_raised_at))
    expect(w.get('[data-testid="stage-hands"]').text()).toContain('No raised hands')
  })
})

describe('the roster of a Talk', () => {
  const props = () => ({ users: world().users, streamOf: () => null, canFocusCamera: () => false, isCameraFocused: () => false })

  it('is the Stage layout for a Stage and the grid for a normal Talk', async () => {
    const p = props()
    const stubs = { StageLayout: { template: '<div data-testid="layout-stage" />' }, TalkGrid: { template: '<div data-testid="layout-grid" />' } }
    wrapper = mount(TalkRoster, { props: { ...p, channelId: room }, global: { stubs } })
    expect(wrapper.find('[data-testid="layout-stage"]').exists()).toBe(true)
    wrapper.unmount()
    useChatStore().uncategorized = [channelFixture({ id: room, type: 'voice' })]
    wrapper = mount(TalkRoster, { props: { ...p, channelId: room }, global: { stubs } })
    expect(wrapper.find('[data-testid="layout-grid"]').exists()).toBe(true)
  })

  it('stays a grid when the server has no Stages', () => {
    const p = props()
    useServerFeaturesStore().list = []
    wrapper = mount(TalkRoster, { props: { ...p, channelId: room }, global: { stubs: { StageLayout: true, TalkGrid: { template: '<div data-testid="layout-grid" />' } } } })
    expect(wrapper.find('[data-testid="layout-grid"]').exists()).toBe(true)
  })
})

describe('reactions in a Stage', () => {
  it('float over the row of a listener too', async () => {
    const { users, voice } = world()
    voice.setChannel(room)
    const w = mountLayout(users)
    voice.handleTalkReaction({ channel_id: room, user_id: 'dan', emoji: '🎉' })
    await nextTick()
    const row = w.findAll('[data-testid="stage-listener"]').find(r => r.text().includes('Dan'))!
    expect(row.find('[data-testid="talk-reaction"]').exists()).toBe(true)
  })
})
