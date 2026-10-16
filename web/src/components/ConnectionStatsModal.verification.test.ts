import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ConnectionStatsModal from './ConnectionStatsModal.vue'
import { useVoiceStore } from '../stores/voice'
import { fixtureId, voiceUserFixture } from '../test-fixtures.fixture'
import { t } from '../i18n'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

describe('ConnectionStatsModal safety code', () => {
  it('says there is no code before the call has keys', () => {
    const wrapper = mount(ConnectionStatsModal)
    expect(wrapper.find('[data-testid="media-safety-code"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="media-verification"]').text()).toContain(t('stats.noSafetyCode'))
  })

  it('shows the code and who holds the keys, flagging anyone not in the Talk or holding two leaves', () => {
    const voice = useVoiceStore()
    const channel = fixtureId(9)
    const alice = voiceUserFixture({ id: fixtureId(1), username: 'alice', display_name: 'Alice' })
    const bob = voiceUserFixture({ id: fixtureId(2), username: 'bob', display_name: '' })
    voice.currentChannelId = channel
    voice.channelUsers = { [channel]: { [alice.id]: alice, [bob.id]: bob } }
    voice.mediaVerification = { epoch: 4, code: '01234 56789 01234 56789', members: ['', alice.id, bob.id, bob.id, fixtureId(3)] }
    const wrapper = mount(ConnectionStatsModal)
    expect(wrapper.get('[data-testid="media-safety-code"]').text()).toBe('01234 56789 01234 56789')
    const members = wrapper.findAll('[data-in-room]')
    expect(members.map(member => [member.text(), member.attributes('data-in-room')])).toEqual([
      [t('stats.unknownKeyMember'), 'false'],
      ['Alice', 'true'],
      ['bob', 'true'],
      ['bob', 'false'],
      [t('stats.unknownKeyMember'), 'false'],
    ])
  })

  it('names nobody once the Talk is left', () => {
    useVoiceStore().mediaVerification = { epoch: 1, code: '00000 00000 00000 00001', members: [fixtureId(1)] }
    const wrapper = mount(ConnectionStatsModal)
    expect(wrapper.get('[data-in-room]').text()).toBe(t('stats.unknownKeyMember'))
  })
})
