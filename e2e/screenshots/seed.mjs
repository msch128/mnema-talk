// Seeds a throwaway Mnema Talk instance with neutral demo data (invented
// people, channels and messages) for the README screenshots. See capture.mjs.
import { chromium } from '@playwright/test'

export const BASE = process.env.SHOTS_BASE_URL || 'http://127.0.0.1:58980'
export const ADMIN_USER = process.env.SHOTS_ADMIN_USER || 'Herzog'
export const ADMIN_PASSWORD = process.env.SHOTS_ADMIN_PASSWORD || ''
export const MEMBER_PASSWORD = 'demo-pass-for-screenshots-only'
export const MEMBERS = ['mira', 'jonas', 'nora', 'felix']

export const api = (page, method, path, body) => page.evaluate(async ({ method, path, body }) => {
  const r = await fetch(path, {
    method, credentials: 'same-origin',
    ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  })
  const json = await r.json().catch(() => null)
  return { status: r.status, json }
}, { method, path, body })

const must = (r, what) => {
  if (r.status >= 300) throw new Error(`${what}: HTTP ${r.status} ${JSON.stringify(r.json)}`)
  return r.json
}

export async function login(page, user, password) {
  await page.goto(BASE + '/')
  must(await api(page, 'POST', '/api/auth/login', { username: user, password }), `login ${user}`)
}

const bit = n => 2 ** n

export async function seed(browser) {
  const ctx = () => browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 900 } })
  const admin = await (await ctx()).newPage()
  await login(admin, ADMIN_USER, ADMIN_PASSWORD)

  // Members.
  const pages = {}
  for (const name of MEMBERS) {
    const p = await (await ctx()).newPage()
    await p.goto(BASE + '/')
    const inv = must(await api(admin, 'POST', '/api/admin/invites', { max_uses: 1 }), 'invite')
    must(await api(p, 'POST', '/api/auth/register', { username: name, password: MEMBER_PASSWORD, invite_code: inv.code }), `register ${name}`)
    pages[name] = p
  }
  const status = { mira: 'Shipping the 1.4 docs', jonas: 'Back at 14:00', nora: 'Reading RFCs' }
  for (const [n, s] of Object.entries(status)) must(await api(pages[n], 'PUT', '/api/users/me/status', { status_text: s }), 'status')

  // Layout. The installation's default channels are replaced by the demo layout.
  const before = must(await api(admin, 'GET', '/api/channels'), 'channels')
  const cat = async (name, sort_order) => must(await api(admin, 'POST', '/api/admin/categories', { name, sort_order }), 'category').id
  const community = await cat('Community', 0)
  const projects = await cat('Projects', 1)
  const talks = await cat('Talks', 2)
  const ch = async (name, type, category_id, sort_order, topic = '') =>
    must(await api(admin, 'POST', '/api/admin/channels', { name, type, category_id, sort_order, topic }), `channel ${name}`).id
  const general = await ch('general', 'text', community, 0, 'Everything that fits nowhere else')
  await ch('announcements', 'text', community, 1, 'Read-mostly news')
  await ch('show-and-tell', 'text', community, 2, 'Things you made this week')
  const devChat = await ch('dev-chat', 'text', projects, 0, 'Code, builds and reviews')
  const releases = await ch('releases', 'text', projects, 1, 'Release notes via webhook')
  const core = await ch('core-team', 'text', projects, 2, 'Private: maintainers only')
  const lounge = await ch('Lounge', 'voice', talks, 0)
  await ch('Game night', 'voice', talks, 1)
  await ch('Standup', 'voice', talks, 2)

  for (const c of before.categories) {
    for (const x of c.channels ?? []) must(await api(admin, 'DELETE', `/api/admin/channels/${x.id}`), 'delete default channel')
    must(await api(admin, 'DELETE', `/api/admin/categories/${c.id}`), 'delete default category')
  }
  for (const x of before.uncategorized) must(await api(admin, 'DELETE', `/api/admin/channels/${x.id}`), 'delete default channel')

  // Roles: Moderator, Builder, Guest; the maintainers' channel is private.
  const role = async (name, color, permissions, hoist) =>
    must(await api(admin, 'POST', '/api/admin/roles', { name, color, permissions, hoist }), `role ${name}`)
  const moderator = await role('Moderator', 0xe0a458, bit(5) + bit(10) + bit(15), true)
  const builder = await role('Builder', 0x2da771, bit(11) + bit(13), true)
  await role('Guest', 0x7d8a84, 0, false)
  const rolesList = must(await api(admin, 'GET', '/api/roles'), 'roles')
  const all = rolesList.roles ?? rolesList
  const idOf = n => all.find(r => r.name === n).id
  const everyone = all.find(r => r.is_everyone).id
  const members = must(await api(admin, 'GET', '/api/members'), 'members')
  const users = members.members ?? members
  const uid = n => users.find(u => (u.username ?? u.display_name) === n).id
  must(await api(admin, 'PUT', `/api/admin/users/${uid('nora')}/roles`, { role_ids: [idOf('Moderator')] }), 'assign')
  must(await api(admin, 'PUT', `/api/admin/users/${uid('mira')}/roles`, { role_ids: [idOf('Builder')] }), 'assign')
  must(await api(admin, 'PUT', `/api/admin/users/${uid('felix')}/roles`, { role_ids: [idOf('Guest')] }), 'assign')
  must(await api(admin, 'PUT', `/api/admin/channels/${core}/overwrites`, { overwrites: [
    { id: everyone, type: 'role', allow: 0, deny: bit(1) },
    { id: idOf('Builder'), type: 'role', allow: bit(1), deny: 0 },
  ] }), 'overwrites')

  // Conversation in #general.
  const say = async (who, channel, content, extra = {}) =>
    must(await api(who === 'admin' ? admin : pages[who], 'POST', `/api/channels/${channel}/messages`, { content, ...extra }), 'message')
  const m1 = await say('mira', general, 'Morning all. The 1.4 docs draft is up, I would love a second pair of eyes on the **upgrade section**.')
  await say('jonas', general, 'On it after standup. Did we settle on the date for the next community call?')
  const m3 = await say('nora', general, 'Yes, Friday at 18:00 in the Lounge. I will pin the agenda in #announcements.')
  await say('admin', general, 'Works for me. I will bring the roadmap notes.', { reply_to_id: m3.id })
  await say('felix', general, 'Quick question from a newcomer: where do I find the safety code for a call?')
  await say('mira', general, 'Open the connection details inside a Talk. If the code matches for everyone, you all hold the same media keys.')
  await say('jonas', general, 'Handy snippet for the review:\n```sh\ndocker compose pull app && docker compose up -d\n```')
  await say('nora', general, 'Game night Talk opens at 20:00. Bring snacks.')
  for (const [who, emoji, msg] of [['jonas', '👍', m1], ['nora', '🎉', m1], ['felix', '👍', m1], ['mira', '👀', m3]]) {
    must(await api(pages[who], 'POST', `/api/messages/${msg.id}/reactions`, { emoji }), 'react')
  }
  await say('jonas', devChat, 'CI is green on main again. Merging the playback fix.')
  await say('mira', devChat, 'Thanks! Cutting the release in ten minutes.')

  // Incoming webhook: a release announcement.
  const hook = must(await api(admin, 'POST', `/api/admin/channels/${releases}/webhooks`, { name: 'Release bot' }), 'webhook')
  const url = hook.url ?? hook.webhook?.url
  if (url) {
    const r = await admin.evaluate(async (u) => {
      const res = await fetch(u, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
        username: 'Release bot',
        embeds: [{ title: 'v1.4.0 published', description: 'Faster search, a new roles editor and 14 fixes.', color: 0x2da771 }],
      }) })
      return res.status
    }, url)
    if (r >= 300) throw new Error(`webhook execute failed: ${r}`)
  }
  // Outgoing webhook (placeholder target, never called by the demo).
  const out = await api(admin, 'POST', `/api/admin/channels/${releases}/outgoing-webhooks`, {
    name: 'Docs rebuild', url: 'https://hooks.example.com/mnema', events: ['MESSAGE_CREATE'],
  })
  if (out.status >= 300) console.warn('outgoing webhook not created:', out.status, JSON.stringify(out.json))

  return { admin, pages, ids: { general, releases, devChat, core, lounge } }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const browser = await chromium.launch()
  await seed(browser)
  await browser.close()
  console.log('seeded')
}
