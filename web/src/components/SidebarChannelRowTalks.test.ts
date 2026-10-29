import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import SidebarChannelRow from './SidebarChannelRow.vue'
import TalkParticipants from './TalkParticipants.vue'
import { useVoiceStore } from '../stores/voice'
import { canConfigureTalk, isTalkLeader, isTempTalk } from '../lib/talks'
import { channelFixture, fixtureId, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

const ann = voiceUserFixture({ id: fixtureId(1), username: 'ann' })
const ben = voiceUserFixture({ id: fixtureId(2), username: 'ben' })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('Talks in the sidebar', () => {
  it('marks the leader of a temporary Talk, and only the leader', () => {
    const talk = channelFixture({ id: fixtureId(5), type: 'voice', name: 'Talk 1', is_temp: true, leader_id: ann.id })
    useVoiceStore().channelUsers = { [talk.id]: { [ann.id]: ann, [ben.id]: ben } }
    const w = mount(SidebarChannelRow, { props: { channel: talk } })
    const rows = w.findAll('[data-voice-user]')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.find('[data-testid="talk-leader-mark"]').exists()).toBe(true)
    expect(rows[1]?.find('[data-testid="talk-leader-mark"]').exists()).toBe(false)
  })

  it('opens a participant with Space as well as Enter', async () => {
    const talk = channelFixture({ id: fixtureId(5), type: 'voice', name: 'Talk 1' })
    useVoiceStore().channelUsers = { [talk.id]: { [ann.id]: ann } }
    const w = mount(SidebarChannelRow, { props: { channel: talk } })
    const row = w.get('[data-voice-user]')
    await row.trigger('keydown', { key: ' ' })
    await row.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('voice-user-click')).toHaveLength(2)
  })

  it('shows a lobby with its own icon and without the Talk chat button', () => {
    const lobby = channelFixture({ id: fixtureId(6), type: 'voice', name: 'Create a Talk', is_lobby: true })
    const w = mount(SidebarChannelRow, { props: { channel: lobby } })
    expect(w.find('[data-testid="lobby-icon"]').exists()).toBe(true)
    expect(w.find('[data-testid="talk-open-chat"]').exists()).toBe(false)
    const plain = mount(SidebarChannelRow, { props: { channel: channelFixture({ type: 'voice' }) } })
    expect(plain.find('[data-testid="lobby-icon"]').exists()).toBe(false)
    expect(plain.find('[data-testid="talk-open-chat"]').exists()).toBe(true)
  })

  it('marks the leader in the participants list of the Talk', async () => {
    const w = mount(TalkParticipants, {
      props: { users: [{ ...ann, joined_at: ann.joined_at }, { ...ben, joined_at: ben.joined_at }], leaderId: ben.id }
    })
    await w.find('[data-testid="talk-participants"]').trigger('click')
    const items = w.findAll('[data-participant]')
    expect(items.find(i => i.attributes('data-participant') === 'ben')?.find('[data-testid="talk-leader-mark"]').exists()).toBe(true)
    expect(items.find(i => i.attributes('data-participant') === 'ann')?.find('[data-testid="talk-leader-mark"]').exists()).toBe(false)
  })
})

describe('who sees the Talk settings', () => {
  const me = userFixture().id
  const voice = channelFixture({ type: 'voice' })
  const led = channelFixture({ type: 'voice', is_temp: true, leader_id: me })

  it('is a manager, or the leader of a temporary Talk, never for a text channel', () => {
    expect(canConfigureTalk(voice, me, true)).toBe(true)
    expect(canConfigureTalk(voice, me, false)).toBe(false)
    expect(canConfigureTalk(led, me, false)).toBe(true)
    expect(canConfigureTalk(channelFixture({ type: 'text' }), me, true)).toBe(false)
    expect(canConfigureTalk(null, me, true)).toBe(false)
  })

  it('reads the leader only from a temporary Talk', () => {
    expect(isTalkLeader(led, me)).toBe(true)
    expect(isTalkLeader(led, fixtureId(99))).toBe(false)
    expect(isTalkLeader(channelFixture({ type: 'voice', leader_id: me }), me)).toBe(false)
    expect(isTempTalk(led)).toBe(true)
    expect(isTempTalk(undefined)).toBe(false)
  })
})
