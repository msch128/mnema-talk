import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import ParticipantTile from './ParticipantTile.vue'

const user = { id: 'u1', username: 'ada', display_name: 'Ada', joined_at: new Date().toISOString() }

function tile(props) {
  return mount(ParticipantTile, {
    props: { user, ...props },
    global: { directives: { tooltip: {} } }
  })
}

describe('ParticipantTile status line', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
  })

  it('shows the time in the Talk while speaking, without a "Speaking" label', () => {
    const w = tile({ speaking: true })
    const status = w.find('[data-tile-status]')
    expect(status.exists()).toBe(true)
    expect(status.text()).not.toMatch(/speaking/i)
    expect(status.text()).toMatch(/\d/)
  })

  it('shows the same status line when silent', () => {
    expect(tile({ speaking: false }).find('[data-tile-status]').text()).toMatch(/\d/)
  })
})
