import type { ChatDMMember, ChatDirectMessage } from './types/rest'
import { FIXTURE_TIMESTAMP, fixtureId } from './test-fixtures.fixture'

/** A member of a direct message conversation. */
export function dmMemberFixture(index: number, overrides: Partial<ChatDMMember> = {}): ChatDMMember {
  return { id: fixtureId(index), username: `member${index}`, display_name: `Member ${index}`, ...overrides }
}

/** A conversation of the caller (fixtureId(1)) with the member fixtureId(2), unless members say otherwise. */
export function dmFixture(overrides: Partial<ChatDirectMessage> = {}): ChatDirectMessage {
  return {
    id: fixtureId(100), group: false, can_send: true, created_at: FIXTURE_TIMESTAMP,
    last_message_at: null, last_read_at: null, unread_count: 0, mention_count: 0, notify_level: 'inherit',
    members: [dmMemberFixture(1), dmMemberFixture(2)],
    ...overrides
  }
}
