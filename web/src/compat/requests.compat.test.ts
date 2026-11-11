import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { effectScope } from 'vue'
import type { ApiOptions } from '../shared/lib/api'
import type { Wire as WireType } from './wire.fixture'
import { Wire } from './wire.fixture'
import { allBaselines } from './baselines'
import { headBaseline } from './head.fixture'
import { newEvents } from './allowlist'
import type { CompatBaseline } from './contract.fixture'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useEventsStore } from '../stores/events'
import { usePermissionsStore } from '../stores/permissions'
import { useReportsOpenStore } from '../stores/reportsOpen'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useChannelLayout } from '../composables/useChannelLayout'
import { moveChannel } from '../shared/lib/channelLayout'
import { liftTimeout } from '../shared/lib/moderationActions'
import { payloadOf, changedFields, fieldsOf } from '../events/lib/eventPayload'
import { PERMISSION_BITS } from '../shared/lib/permissions'
import { categoryFixture, channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import AdminVanityLink from '../components/AdminVanityLink.vue'
import ProfileModerationTab from '../components/ProfileModerationTab.vue'

// The current web app against what each older server release promised: every
// request it sends for these flows must fit that release's contract, and every
// endpoint it uses must exist there or be allowlisted with a fallback.
const server = vi.hoisted(() => ({ wire: null as WireType | null }))
vi.mock('../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../shared/lib/api')>()),
  api: (path: string, options?: ApiOptions) => required(server.wire).handle(path, options)
}))

const ch = channelFixture({ id: fixtureId(2) })
const me = userFixture({ id: fixtureId(1), username: 'me', presence: 'online' })
const tick = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }

class Socket {
  static latest: Socket | null = null
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn<(data: string) => void>()
  close = vi.fn()
  constructor(public url: string) { Socket.latest = this }
}

let consoleError: ReturnType<typeof vi.spyOn>

/** A web app talking to a server of that release, which lists the features that release listed. */
function boot(baseline: CompatBaseline, extraFeatures: string[] = []): WireType {
  setActivePinia(createPinia())
  const wire = new Wire(baseline)
  server.wire = wire
  const features = useServerFeaturesStore()
  features.list = [...(baseline.health.features ?? []), ...extraFeatures]
  features.loaded = true
  return wire
}

function chatSetup() {
  const chat = useChatStore()
  useAuthStore().user = { ...me }
  chat.activeChannel = { ...ch }
  chat.uncategorized = [ch]
  chat.members = [me]
  return chat
}

function expectCompatible(wire: WireType) {
  expect(wire.violations).toEqual([])
  expect(consoleError).not.toHaveBeenCalled()
}

beforeEach(() => {
  localStorage.clear()
  consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const releases = [...allBaselines, headBaseline()]

describe.each(releases)('requests to server $version', baseline => {
  const has = (feature: string) => baseline.health.features?.includes(feature) ?? false

  it('sends text, replies and thread replies as that release takes them', async () => {
    const wire = boot(baseline)
    const chat = chatSetup()
    await chat.sendMessage('plain', null, null, [], ch.id)
    await chat.sendMessage('reply', null, fixtureId(30), [], ch.id)
    await chat.sendMessage('thread', fixtureId(31), null, [], ch.id)
    await chat.sendMessage('thread reply', fixtureId(31), fixtureId(32), [], ch.id)
    const posts = wire.callsTo(/^POST \/api\/channels\/[^/]+\/messages$/)
    expect(posts).toHaveLength(4)
    expect(posts.map(p => 'nonce' in (p.json as object))).toEqual(Array(4).fill(has('messages.nonce')))
    expectCompatible(wire)
  })

  it('keeps a message written offline and settles it as that release allows once the connection returns', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', Socket)
    vi.stubGlobal('Notification', undefined)
    const wire = boot(baseline)
    const chat = chatSetup()
    wire.down = true
    await chat.sendMessage('written on the train', null, null, [], ch.id)
    expect(chat.outbox.map(p => p.status)).toEqual(['waiting'])
    wire.down = false
    chat.initWebSocket()
    required(Socket.latest).onopen?.()
    await tick()
    const delivered = wire.callsTo(/^POST \/api\/channels\/[^/]+\/messages$/).filter(c => !wire.down && c.json !== undefined).length
    if (has('messages.nonce')) {
      // The nonce makes the repeat safe: it is sent and the outbox is empty.
      expect(chat.outbox).toEqual([])
      expect(delivered).toBeGreaterThanOrEqual(2)
    } else {
      // A repeat would post a second copy: the member decides.
      expect(chat.outbox.map(p => p.status)).toEqual(['failed'])
    }
    chat.closeWebSocket()
    expectCompatible(wire)
  })

  it.runIf(has('reports'))('counts the open reports for a moderator: by the open-count route where the release lists it, else from the list of open reports', async () => {
    const wire = boot(baseline)
    useAuthStore().user = { ...me }
    usePermissionsStore().info = { owner: false, permissions: PERMISSION_BITS.manage_reports, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
    const reports = useReportsOpenStore()
    await flushPromises()
    expect(wire.callsTo(/open-count$/)).toHaveLength(has('reports.open_count') ? 1 : 0)
    expect(wire.callsTo(/^GET \/api\/admin\/reports\?status=open&limit=100$/)).toHaveLength(has('reports.open_count') ? 0 : 1)
    expect(reports.count).toBe(0)
    expectCompatible(wire)
  })

  it.runIf(has('events'))('creates and edits an event (with a series when the release can repeat)', async () => {
    const wire = boot(baseline)
    const events = useEventsStore()
    const fields = { ...fieldsOf(null), title: 'Game night', startTime: '2026-03-01T20:00', locationText: 'Lobby', repeat: has('events.recurrence') ? 'weekly' : '' }
    const created = await events.create(payloadOf(fields, has('events.recurrence')))
    await events.update(created.id, changedFields(payloadOf({ ...fields, title: 'Game night 2' }, false), created))
    expect(wire.callsTo(/^POST \/api\/events$/)).toHaveLength(1)
    expect(wire.callsTo(/^PATCH \/api\/events\/[^/]+$/)).toHaveLength(1)
    expectCompatible(wire)
  })

  it('saves the vanity link', async () => {
    const wire = boot(baseline)
    useAuthStore().user = userFixture({ role: 'admin' })
    const w = mount(AdminVanityLink)
    await flushPromises()
    if (w.find('[data-testid="vanity-slug"]').exists()) {
      await w.get('[data-testid="vanity-slug"]').setValue('mein-clan')
      await w.get('form').trigger('submit')
      await flushPromises()
      expect(wire.callsTo(/^PUT \/api\/admin\/vanity-link$/)).toHaveLength(1)
    }
    expectCompatible(wire)
    w.unmount()
  })

  it.runIf(has('moderation.history'))('edits a moderation note', async () => {
    const wire = boot(baseline)
    wire.answer(/^GET \/api\/admin\/users\/[^/]+\/moderation$/, answer => {
      const history = answer as { notes: Array<Record<string, unknown>> }
      history.notes.forEach(note => Object.assign(note, { mine: true, body: 'old' }))
      return history
    }, 'full')
    const w = mount(ProfileModerationTab, { props: { userId: fixtureId(7) } })
    await flushPromises()
    await w.get('[data-testid="mod-note-edit"]').trigger('click')
    await w.get('[data-testid="mod-note-edit-input"]').setValue('new')
    await w.get('[data-testid="mod-note-save"]').trigger('click')
    await flushPromises()
    expect(wire.callsTo(/^PUT \/api\/admin\/users\/[^/]+\/moderation\/notes\/[^/]+$/)).toHaveLength(1)
    expectCompatible(wire)
    w.unmount()
  })

  it('saves the channel layout and lifts a timeout', async () => {
    const wire = boot(baseline)
    const chat = useChatStore()
    chat.categories = [categoryFixture({ id: fixtureId(10), name: 'A', sort_order: 0, channels: [channelFixture({ id: fixtureId(11), category_id: fixtureId(10) })] })]
    chat.uncategorized = [channelFixture({ id: fixtureId(12), category_id: null })]
    const scope = effectScope()
    const { layout, commit } = required(scope.run(() => useChannelLayout()))
    await commit(moveChannel(layout.value, fixtureId(12), fixtureId(10), 0), { toast: 'moved' })
    expect(await liftTimeout({ id: fixtureId(9) })).toBe(true)
    expect(wire.callsTo(/^PUT \/api\/admin\/layout$/)).toHaveLength(1)
    expect(wire.callsTo(/^DELETE \/api\/admin\/users\/[^/]+\/timeout$/)).toHaveLength(1)
    expectCompatible(wire)
    scope.stop()
  })

  it('creates a poll (anonymous where the release has them)', async () => {
    const wire = boot(baseline)
    const chat = chatSetup()
    await chat.createPoll(ch.id, { question: 'Pizza?', options: ['yes', 'no'], ...(has('polls.anonymous') ? { anonymous: true } : {}) })
    expect(wire.callsTo(/^POST \/api\/channels\/[^/]+\/polls$/)).toHaveLength(1)
    expectCompatible(wire)
  })
})

describe('the harness notices what it should', () => {
  const v2918 = required(allBaselines.find(b => b.version === 'v2.9.18'))

  it('flags a nonce sent to a release that rejects unknown fields', async () => {
    const wire = boot(v2918, ['messages.nonce'])
    await chatSetup().sendMessage('hi', null, null, [], ch.id)
    expect(wire.violations).toEqual([expect.stringContaining('chat.CreateMessageRequest.nonce: unknown field')])
  })

  it('flags the open-count route on a release that reads it as a report id', async () => {
    const wire = boot(v2918, ['reports.open_count'])
    useAuthStore().user = { ...me }
    usePermissionsStore().info = { owner: false, permissions: PERMISSION_BITS.manage_reports, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
    useReportsOpenStore()
    await flushPromises()
    expect(wire.violations).toEqual([expect.stringContaining('GET /api/admin/reports/open-count: no such endpoint')])
  })
})

describe('events', () => {
  it('names every event added since the newest older release in the allowlist', () => {
    const oldest = required(allBaselines.at(-1))
    const added = headBaseline().eventTypes.filter(type => !oldest.eventTypes.includes(type))
    expect(added.filter(type => !newEvents.includes(type)), 'new events need an allowlist entry (an old server never sends them: say what the app does without)').toEqual([])
    expect(newEvents.filter(type => !added.includes(type)), 'stale allowlist entries').toEqual([])
  })
})
