import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import SidebarChannelRow from './SidebarChannelRow.vue'
import { useChatStore } from '../stores/chat'
import { channelFixture, fixtureId, readStateFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

const text = channelFixture({ id: fixtureId(3), name: 'design', type: 'text' })
const voice = channelFixture({ id: fixtureId(4), name: 'Lounge', type: 'voice' })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

function readState(channelId: string, counts: { unread?: number; mentions?: number }) {
  useChatStore().readStates = { [channelId]: readStateFixture({ channel_id: channelId, unread_count: counts.unread ?? 0, mention_count: counts.mentions ?? 0 }) }
}

const mountRow = (channel = text, admin = false) => mount(SidebarChannelRow, { props: { channel, admin }, global: { directives: { tooltip: () => undefined } } })

describe('channel row badges', () => {
  it('shows a mention as the red badge and plain unread as the neutral pill', () => {
    readState(text.id, { unread: 2 })
    const unread = mountRow().get('[data-testid="channel-unread"]')
    expect(unread.text()).toBe('2')
    expect(unread.classes()).toContain('bg-mnema-hover')
    expect(unread.classes()).not.toContain('bg-mnema-danger')

    readState(text.id, { unread: 2, mentions: 1 })
    const w = mountRow()
    expect(w.get('[data-testid="channel-mentions"]').classes()).toContain('bg-mnema-danger')
    expect(w.find('[data-testid="channel-unread"]').exists()).toBe(false)
  })

  it('keeps the badge in the same place for members and admins', () => {
    readState(text.id, { unread: 3 })
    const member = mountRow(text, false).get('[role="button"]')
    const admin = mountRow(text, true).get('[role="button"]')
    expect(member.classes().join(' ')).not.toMatch(/\bpr-/)
    expect(admin.classes().join(' ')).not.toMatch(/\bpr-/)
  })

  it('lets the buttons beside the row take the badge place while the pointer is on it', () => {
    readState(text.id, { unread: 3 })
    const slot = (w: ReturnType<typeof mountRow>) => w.get('[data-testid="channel-unread"]').element.parentElement
    expect(slot(mountRow(text, true))?.className).toContain('md:group-hover/row:invisible')
    expect(slot(mountRow(text, false))?.className).not.toContain('invisible')
    readState(voice.id, { unread: 1 })
    expect(slot(mountRow(voice))?.className).toContain('md:group-hover/row:invisible')
  })

  it('shows the delete button only under the pointer', () => {
    const w = mountRow(text, true)
    const del = w.get('button[aria-label="Delete channel"]')
    expect(del.classes()).toContain('opacity-0')
    expect(del.classes()).toContain('group-hover/row:opacity-100')
  })
})
