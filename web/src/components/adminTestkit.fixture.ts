import type { VueWrapper } from '@vue/test-utils'
import type { AdminUser, ChannelHierarchy } from '../types/domain'
import { useToastStore } from '../stores/toast'
import { categoryFixture, channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'

/** Channel layout with uncategorized and categorized text/voice channels. */
export function tree(): ChannelHierarchy {
  return {
    uncategorized: [
      channelFixture({ id: fixtureId(10) }),
      channelFixture({ id: fixtureId(11), type: 'voice' }),
    ],
    categories: [
      categoryFixture({
        id: fixtureId(20),
        channels: [
          channelFixture({ id: fixtureId(21), category_id: fixtureId(20), topic: 'topic' }),
          channelFixture({ id: fixtureId(22), category_id: fixtureId(20), type: 'voice' }),
        ],
      }),
      categoryFixture({ id: fixtureId(30), sort_order: 1 }),
    ],
  }
}

/** An active (or disabled) member as the admin user list returns it. */
export function member(disabled = false): AdminUser {
  return {
    ...userFixture({
      id: fixtureId(disabled ? 42 : 41),
      username: disabled ? 'disabled' : 'member',
      display_name: '',
    }),
    disabled,
    banned: false,
    last_seen_at: disabled ? null : '2026-01-01T00:00:00Z',
  }
}

/** Texts of the error toasts shown so far. */
export function errors() {
  return useToastStore()
    .toasts.filter((t) => t.type === 'error')
    .map((t) => t.text)
}

export function button(w: VueWrapper, text: string) {
  const b = w.findAll('button').find((b) => b.text() === text)
  if (!b) throw new Error(`Missing button ${text}`)
  return b
}
