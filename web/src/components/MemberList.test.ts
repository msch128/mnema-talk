import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import MemberList from './MemberList.vue'
import MemberRow from './MemberRow.vue'
import ContextMenu from './ContextMenu.vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { categoryFixture, channelFixture, fixtureId, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Role } from '../types/domain'

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(40 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const owner = userFixture({ id: fixtureId(1), username: 'herzog', display_name: 'Herzog', role: 'admin' })
const ann = userFixture({ id: fixtureId(2), username: 'ann', display_name: 'Ann' })
const bob = userFixture({ id: fixtureId(3), username: 'bob', display_name: 'Bob' })
const cy = userFixture({ id: fixtureId(4), username: 'cy', display_name: 'Cy' })
const dee = userFixture({ id: fixtureId(5), username: 'dee', display_name: 'Dee' })

function setup() {
  const chat = useChatStore()
  chat.members = [owner, ann, bob, cy, dee]
  chat.presenceById = { [owner.id]: 'online', [ann.id]: 'online', [bob.id]: 'online', [cy.id]: 'online' }
  const permissions = usePermissionsStore()
  permissions.roles = [role(3, { name: 'Crew', hoist: true, color: 0xff0000 }), role(2, { name: 'Quiet', hoist: false }), role(1, { name: 'Guests', hoist: true }), role(0, { name: '@everyone', is_everyone: true, hoist: true })]
  permissions.setMemberRoles([{ id: ann.id, role_ids: [fixtureId(43), fixtureId(41)] }, { id: bob.id, role_ids: [fixtureId(42)] }, { id: cy.id, role_ids: [fixtureId(41)] }, { id: dee.id, role_ids: [fixtureId(43)] }])
  return mount(MemberList)
}
const groups = (w: ReturnType<typeof mount>) => w.findAll('section').map(s => s.find('h3').text())

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('MemberList', () => {
  it('groups the Owner, hoisted roles, the rest online, and offline members', () => {
    const w = setup()
    expect(groups(w)).toEqual(['Owner', 'Crew – 1', 'Guests – 1', 'Online – 1', 'Offline – 1'])
    expect(w.find('[data-role-group="Crew"]').text()).toContain('Ann')
    expect(w.find('[data-role-group="Guests"]').text()).toContain('Cy')
    expect(w.findAll('section').at(3)!.text()).toContain('Bob')
    // a member's name takes the colour of their highest coloured role
    expect(w.find('[data-member="ann"] span.truncate').attributes('style')).toContain('#ff0000')
    expect(w.find('[data-member="bob"] span.truncate').attributes('style')).toBeUndefined()
    // offline members keep the muted look, whatever their role colour
    expect(w.find('[data-member="dee"] span.truncate').attributes('style')).toBeUndefined()
  })

  it('has no role groups without hoisted roles', () => {
    const w = setup()
    usePermissionsStore().roles = []
    return w.vm.$nextTick().then(() => expect(groups(w)).toEqual(['Owner', 'Online – 3', 'Offline – 1']))
  })

  it('shows where a member is in a Talk, by channel name', async () => {
    const chat = useChatStore()
    const voice = useVoiceStore()
    const talk = channelFixture({ id: fixtureId(60), type: 'voice', name: 'Lounge', category_id: fixtureId(61) })
    const stray = channelFixture({ id: fixtureId(62), type: 'voice', name: 'Loose' })
    chat.categories = [categoryFixture({ id: fixtureId(61), channels: [talk] })]
    chat.uncategorized = [stray]
    const w = setup()
    voice.channelUsers = {
      [talk.id]: { [ann.id]: voiceUserFixture({ id: ann.id }) },
      [stray.id]: { [bob.id]: voiceUserFixture({ id: bob.id }) },
      [fixtureId(63)]: { [cy.id]: voiceUserFixture({ id: cy.id }) }
    }
    await w.vm.$nextTick()
    expect(w.find('[data-member="ann"]').text()).toContain('Lounge')
    expect(w.find('[data-member="bob"]').text()).toContain('Loose')
    expect(w.find('[data-member="cy"]').text()).toContain('Talk')
  })

  it('opens the member menu from a context click', async () => {
    const w = setup()
    await w.find('[data-member="bob"]').trigger('contextmenu')
    await w.vm.$nextTick()
    expect(w.findComponent(ContextMenu).props('items')?.length).toBeGreaterThan(0)
    w.findAllComponents(MemberRow)[0]?.vm.$emit('menu', new MouseEvent('contextmenu'))
  })
})
