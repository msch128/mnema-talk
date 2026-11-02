// Events with roles for the tests of the roster, its editor and the store.
import type { CommunityEvent } from './types/domain'
import type { CommunityeventsEventRole as EventRole, CommunityeventsSignupItem as SignupItem } from './types/rest'
import { fixtureId, FIXTURE_TIMESTAMP } from './test-fixtures.fixture'

export function roleFixture(n: number, overrides: Partial<EventRole> = {}): EventRole {
  return { id: fixtureId(100 + n), name: `Role ${n}`, position: n, confirmed: 0, waitlist: 0, ...overrides }
}

export function signupFixture(userN: number, roleId: string, state: 'confirmed' | 'waitlist' = 'confirmed', name = `user${userN}`): SignupItem {
  return {
    user: { id: fixtureId(userN), username: name, display_name: name },
    role_id: roleId, state, created_at: FIXTURE_TIMESTAMP
  }
}

export function eventWithRoles(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1), title: 'Raid night', description: '', start_time: '2026-11-01T18:00:00Z', status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP, updated_at: FIXTURE_TIMESTAMP, rsvp_counts: { going: 0, interested: 0, not_going: 0 },
    roles: [], ...overrides
  }
}
