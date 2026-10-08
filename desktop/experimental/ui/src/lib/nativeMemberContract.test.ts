import { expect, it } from 'vitest'
import receipt from '../fixtures/native-members.json'
import { isAuthUser } from '../types/rest'
import { decodeServerEvent } from '../types/events'
it('accepts the actual native Rust PG/TLS member response without inventing a presence', () => {
  expect(receipt.statuses).toEqual([200, 200, 200])
  expect(receipt.members).toHaveLength(2)
  for (const member of receipt.members) {
    expect(Object.hasOwn(member, 'presence')).toBe(false)
    expect(isAuthUser(member)).toBe(true)
    expect(isAuthUser({ ...member, presence: '' })).toBe(false)
  }
})
it('accepts the same native metadata users for live membership and profile updates', () => {
  for (const member of receipt.members) {
    for (const type of ['member_joined', 'user_update'] as const) {
      expect(decodeServerEvent({ type, payload: member })).toEqual({ type, payload: member })
      expect(() => decodeServerEvent({ type, payload: { ...member, presence: '' } })).toThrow()
    }
  }
})
