import type { Channel, Message, User, VoiceUser, Category, ReadState, MediaAttachment } from './types/domain'

export const FIXTURE_TIMESTAMP = '2026-01-01T00:00:00Z'

export function requireValue<T>(value: T | null | undefined, message = 'Expected synthetic test value'): T {
  if (value === undefined || value === null) throw new Error(message)
  return value
}

export function memoryStorageFixture(initial: Record<string, string> = {}) {
  const data = { ...initial }
  return {
    data,
    getItem(key: string): string | null { return data[key] ?? null },
    setItem(key: string, value: string): void { data[key] = String(value) },
    removeItem(key: string): void { delete data[key] },
  }
}

/** Deterministic, fictional UUIDs that satisfy the actual wire identifier guard. */
export function fixtureId(index = 1): string {
  if (!Number.isSafeInteger(index) || index < 1 || index > 0xffffffffffff) {
    throw new RangeError('Fixture ID index must be a positive 48-bit integer')
  }
  return `00000000-0000-4000-8000-${index.toString(16).padStart(12, '0')}`
}

/** Complete REST fixtures; overrides retain each contract's optional/null rules. */
export function userFixture(overrides: Partial<User> = {}): User {
  return {
    id: fixtureId(1), username: 'member', display_name: '', role: 'user',
    bio: '', status_text: '', locale: 'de', created_at: FIXTURE_TIMESTAMP,
    ...overrides,
  }
}

export function channelFixture(overrides: Partial<Channel> = {}): Channel {
  return {
    id: fixtureId(2), name: 'general', type: 'text', topic: '',
    category_id: null, sort_order: 0, created_at: FIXTURE_TIMESTAMP,
    ...overrides,
  }
}

export function messageFixture(overrides: Partial<Message> = {}): Message {
  return {
    id: fixtureId(3), channel_id: fixtureId(2), user_id: fixtureId(1),
    username: 'member', display_name: '', content: 'Synthetic message',
    attachments: [], mentions: [], reactions: [], is_edited: false,
    is_pinned: false, reply_count: 0, created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    ...overrides,
  }
}

export function voiceUserFixture(overrides: Partial<VoiceUser> = {}): VoiceUser {
  return { ...userFixture(), joined_at: FIXTURE_TIMESTAMP, muted: false, deafened: false, ...overrides }
}

export function categoryFixture(overrides: Partial<Category> = {}): Category {
  return { id: fixtureId(4), name: 'Synthetic category', sort_order: 0, created_at: FIXTURE_TIMESTAMP, channels: [], ...overrides }
}

export function readStateFixture(overrides: Partial<ReadState> = {}): ReadState {
  return { channel_id: fixtureId(2), last_read_at: null, mention_count: 0, notify_level: 'all', unread_count: 0, ...overrides }
}

export function attachmentFixture(overrides: Partial<MediaAttachment> = {}): MediaAttachment {
  return {
    id: fixtureId(5), is_deleted: false, mime_type: 'image/png',
    original_filename: 'synthetic.png', size_bytes: 1,
    url: `/api/media/${fixtureId(5)}`, ...overrides,
  }
}
