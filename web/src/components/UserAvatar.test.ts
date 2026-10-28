import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import UserAvatar from './UserAvatar.vue'

describe('UserAvatar', () => {
  it('shows a whole emoji as the initial', () => {
    const w = mount(UserAvatar, { props: { user: { id: 'u1', username: 'fox', display_name: '\u{1F98A} Fox' } as never } })
    expect(w.text()).toContain('\u{1F98A}')
    expect(w.text()).not.toContain('�')
  })
})
