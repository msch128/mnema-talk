import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarChannelRow from './SidebarChannelRow.vue'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { channelFixture, fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

const talk = channelFixture({ id: fixtureId(5), type: 'voice', name: 'Talk 1' })
const mountRow = (channel = talk) => mount(SidebarChannelRow, { props: { channel }, global: { directives: { tooltip: () => undefined } } })

function holds(channelPermissions: number) {
  usePermissionsStore().info = {
    owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [],
    channels: [{ channel_id: talk.id, permissions: channelPermissions }]
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('a Talk the member may not join', () => {
  it('shows a lock instead of the speaker before a click is refused', () => {
    holds(PERMISSION_BITS.view_channel)
    expect(mountRow().find('[data-testid="talk-locked-icon"]').exists()).toBe(true)
  })

  it('shows the usual icon when joining is allowed', () => {
    holds(PERMISSION_BITS.view_channel | PERMISSION_BITS.connect)
    expect(mountRow().find('[data-testid="talk-locked-icon"]').exists()).toBe(false)
  })

  it('never locks a text channel', () => {
    holds(PERMISSION_BITS.view_channel)
    expect(mountRow(channelFixture({ id: talk.id, type: 'text', name: 'general' })).find('[data-testid="talk-locked-icon"]').exists()).toBe(false)
  })
})
