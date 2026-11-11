import { describe, expect, it } from 'vitest'
import { eventAvatar } from './eventAvatar'

describe('eventAvatar', () => {
  it('turns the API reference into what UserAvatar takes, with no null picture', () => {
    expect(eventAvatar({ id: 'u1', username: 'alice', display_name: 'Alice', avatar_url: null })).toEqual({
      display_name: 'Alice', username: 'alice', avatar_url: undefined
    })
    expect(eventAvatar({ id: 'u2', username: 'bob', display_name: 'Bob', avatar_url: '/a.png' }).avatar_url).toBe('/a.png')
  })
})
