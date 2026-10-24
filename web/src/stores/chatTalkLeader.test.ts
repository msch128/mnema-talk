import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useServerFeaturesStore } from './serverFeatures'
import { categoryFixture, channelFixture, fixtureId } from '../test-fixtures.fixture'

const category = fixtureId(1)
const talk = fixtureId(2)
const other = fixtureId(3)
const alice = fixtureId(4)
const bob = fixtureId(5)

beforeEach(() => { setActivePinia(createPinia()) })

function seed() {
  const chat = useChatStore()
  chat.categories = [categoryFixture({
    id: category,
    channels: [channelFixture({ id: talk, type: 'voice', is_temp: true, leader_id: alice }), channelFixture({ id: other, type: 'voice' })]
  })]
  chat.uncategorized = [channelFixture({ id: fixtureId(6), type: 'voice', is_temp: true, leader_id: alice })]
  return chat
}

describe('talk_leader', () => {
  it('moves the leader mark to the new leader of that Talk only', () => {
    const chat = seed()
    chat.handleWSEvent({ type: 'talk_leader', payload: { channel_id: talk, leader_id: bob } })
    const talks = chat.categories[0]?.channels
    expect(talks?.find(c => c.id === talk)?.leader_id).toBe(bob)
    expect(talks?.find(c => c.id === other)?.leader_id).toBeUndefined()
    expect(chat.uncategorized[0]?.leader_id).toBe(alice)
  })

  it('clears the mark when nobody leads the Talk', () => {
    const chat = seed()
    chat.handleWSEvent({ type: 'talk_leader', payload: { channel_id: talk, leader_id: null } })
    expect(chat.allChannels.find(c => c.id === talk)?.leader_id).toBeUndefined()
  })

  it('follows a Talk outside any category and ignores an unknown one', () => {
    const chat = seed()
    chat.handleWSEvent({ type: 'talk_leader', payload: { channel_id: fixtureId(6), leader_id: bob } })
    expect(chat.uncategorized[0]?.leader_id).toBe(bob)
    const before = chat.categories
    chat.handleWSEvent({ type: 'talk_leader', payload: { channel_id: fixtureId(99), leader_id: bob } })
    expect(chat.categories).toBe(before)
  })
})

describe('the talks.lobby feature', () => {
  it('is on only when the server lists it', async () => {
    const features = useServerFeaturesStore()
    expect(features.talksLobby).toBe(false)
    features.list = ['dm', 'talks.lobby']
    expect(features.talksLobby).toBe(true)
  })
})
