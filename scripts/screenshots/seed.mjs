/* global process */
// Seeds a throwaway Mnema Talk instance with one invented community for the
// README screenshots: Lantern Fox Studio, a small indie game studio with eight
// members, roles, channels, a conversation about its game, custom emoji, a
// poll, pins, a thread and a build-bot webhook. Nobody in it is real; the
// avatars, emoji and the game picture are drawn here. See capture.mjs.
import { createRequire } from 'node:module'
import { execFileSync } from 'node:child_process'
// Playwright comes from e2e/ (one install, one pinned version).
const { chromium } = createRequire(new URL('../../e2e/package.json', import.meta.url))('@playwright/test')

export const BASE = process.env.SHOTS_BASE_URL || 'http://127.0.0.1:58980'
export const ADMIN_USER = process.env.SHOTS_ADMIN_USER || 'maren'
export const ADMIN_PASSWORD = process.env.SHOTS_ADMIN_PASSWORD || ''
export const MEMBER_PASSWORD = 'demo-pass-for-screenshots-only'

// username, display name, status line, avatar colour (hue)
export const PEOPLE = [
  ['ines', 'Ines Carvalho', 'Moderating the playtest queue', 340],
  ['theo', 'Theo Brandt', 'Lighting, always lighting', 28],
  ['priya', 'Priya Nair', 'Hunting the save bug', 175],
  ['kai', 'Kai Mori', 'Painting the cave level', 265],
  ['leo', 'Leo Fischer', '', 205],
  ['sam', 'Sam Okafor', 'Playtest on Saturday', 95],
  ['noor', 'Noor Haddad', '', 15],
]
export const OWNER_DISPLAY = 'Maren Vogt'
export const MEMBERS = PEOPLE.map(p => p[0])

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

// Pictures are drawn in the page (canvas) by the functions below and sent as a
// multipart upload. The drawing source is installed in the page as a function,
// never assembled from data.
async function uploadDrawn(page, path, field, name, w, h, draw, opts = {}, extra = {}) {
  await page.evaluate(`window.__draw = ${String(draw)}`)
  return page.evaluate(async ({ path, field, name, w, h, opts, extra }) => {
    const canvas = document.createElement('canvas')
    canvas.width = w; canvas.height = h
    window.__draw(canvas.getContext('2d'), w, h, opts)
    const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'))
    const form = new FormData()
    form.append(field, blob, name)
    for (const [k, v] of Object.entries(extra)) form.append(k, v)
    const r = await fetch(path, { method: 'POST', body: form, credentials: 'same-origin' })
    return { status: r.status, json: await r.json().catch(() => null) }
  }, { path, field, name, w, h, opts, extra })
}

const DRAW = {
  avatar: (ctx, w, h, { hue, letters }) => {
    const g = ctx.createLinearGradient(0, 0, w, h)
    g.addColorStop(0, `hsl(${hue} 55% 38%)`); g.addColorStop(1, `hsl(${(hue + 40) % 360} 60% 22%)`)
    ctx.fillStyle = g; ctx.fillRect(0, 0, w, h)
    ctx.fillStyle = `hsla(${hue} 80% 85% / 0.18)`
    ctx.beginPath(); ctx.arc(w * 0.78, h * 0.2, w * 0.38, 0, Math.PI * 2); ctx.fill()
    ctx.fillStyle = `hsl(${hue} 70% 92%)`; ctx.font = `600 ${Math.round(h * 0.42)}px Inter, system-ui, sans-serif`
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle'; ctx.fillText(letters, w / 2, h * 0.54)
  },
  gg: (ctx) => {
    ctx.fillStyle = '#2da771'; ctx.beginPath(); ctx.roundRect(8, 24, 112, 80, 22); ctx.fill()
    ctx.fillStyle = '#0e1311'; ctx.font = '800 54px Inter, system-ui, sans-serif'; ctx.textAlign = 'center'; ctx.textBaseline = 'middle'; ctx.fillText('GG', 64, 66)
  },
  shipit: (ctx) => {
    ctx.fillStyle = '#e8ebe9'; ctx.beginPath(); ctx.ellipse(64, 60, 20, 44, 0, 0, Math.PI * 2); ctx.fill()
    ctx.fillStyle = '#e0a458'; ctx.beginPath(); ctx.moveTo(64, 8); ctx.quadraticCurveTo(88, 30, 84, 52); ctx.lineTo(44, 52); ctx.quadraticCurveTo(40, 30, 64, 8); ctx.fill()
    ctx.fillStyle = '#3b8fd9'; ctx.beginPath(); ctx.arc(64, 66, 9, 0, Math.PI * 2); ctx.fill()
    ctx.fillStyle = '#c4553a'; ctx.beginPath(); ctx.moveTo(44, 84); ctx.lineTo(24, 110); ctx.lineTo(48, 100); ctx.fill()
    ctx.beginPath(); ctx.moveTo(84, 84); ctx.lineTo(104, 110); ctx.lineTo(80, 100); ctx.fill()
    ctx.fillStyle = '#f2b441'; ctx.beginPath(); ctx.moveTo(52, 104); ctx.lineTo(64, 126); ctx.lineTo(76, 104); ctx.fill()
  },
  bug: (ctx) => {
    ctx.strokeStyle = '#1d3a2a'; ctx.lineWidth = 6; ctx.lineCap = 'round'
    for (const y of [46, 66, 86]) { ctx.beginPath(); ctx.moveTo(24, y + 8); ctx.lineTo(104, y + 8); ctx.stroke() }
    ctx.fillStyle = '#5fbf6a'; ctx.beginPath(); ctx.ellipse(64, 72, 30, 40, 0, 0, Math.PI * 2); ctx.fill()
    ctx.fillStyle = '#2f7d3a'; ctx.beginPath(); ctx.arc(64, 30, 18, 0, Math.PI * 2); ctx.fill()
    ctx.fillStyle = '#0e1311'; ctx.beginPath(); ctx.arc(57, 28, 4, 0, 7); ctx.arc(71, 28, 4, 0, 7); ctx.fill()
    ctx.strokeStyle = '#0e1311'; ctx.lineWidth = 4; ctx.beginPath(); ctx.moveTo(64, 50); ctx.lineTo(64, 110); ctx.stroke()
  },
  lantern: (ctx) => {
    ctx.strokeStyle = '#8e9c94'; ctx.lineWidth = 6; ctx.beginPath(); ctx.arc(64, 22, 14, Math.PI, 0); ctx.stroke()
    ctx.fillStyle = '#3a3340'; ctx.fillRect(40, 34, 48, 10); ctx.fillRect(40, 100, 48, 10)
    const g = ctx.createRadialGradient(64, 72, 4, 64, 72, 34); g.addColorStop(0, '#fff2c0'); g.addColorStop(1, '#f2a03c')
    ctx.fillStyle = g; ctx.beginPath(); ctx.roundRect(42, 44, 44, 56, 10); ctx.fill()
  },
  fox: (ctx) => {
    ctx.fillStyle = '#e8803a'; ctx.beginPath(); ctx.moveTo(14, 18); ctx.lineTo(50, 40); ctx.lineTo(78, 40); ctx.lineTo(114, 18); ctx.lineTo(110, 80); ctx.quadraticCurveTo(64, 126, 18, 80); ctx.fill()
    ctx.fillStyle = '#f6efe6'; ctx.beginPath(); ctx.moveTo(30, 78); ctx.quadraticCurveTo(64, 126, 98, 78); ctx.quadraticCurveTo(64, 92, 30, 78); ctx.fill()
    ctx.fillStyle = '#1b1630'; ctx.beginPath(); ctx.arc(44, 66, 6, 0, 7); ctx.arc(84, 66, 6, 0, 7); ctx.arc(64, 96, 6, 0, 7); ctx.fill()
  },
  star: (ctx) => {
    ctx.fillStyle = '#f2b441'; ctx.beginPath()
    for (let i = 0; i < 10; i++) { const a = -Math.PI / 2 + i * Math.PI / 5; const r = i % 2 ? 26 : 56; ctx.lineTo(64 + Math.cos(a) * r, 68 + Math.sin(a) * r) }
    ctx.fill()
  },
  heart: (ctx) => {
    ctx.fillStyle = '#e0556b'; ctx.beginPath(); ctx.moveTo(64, 108); ctx.bezierCurveTo(8, 70, 20, 20, 64, 46); ctx.bezierCurveTo(108, 20, 120, 70, 64, 108); ctx.fill()
  },
  coffee: (ctx) => {
    ctx.fillStyle = '#e8ebe9'; ctx.beginPath(); ctx.roundRect(22, 44, 66, 58, [6, 6, 26, 26]); ctx.fill()
    ctx.strokeStyle = '#e8ebe9'; ctx.lineWidth = 9; ctx.beginPath(); ctx.arc(94, 70, 15, -1.2, 1.2); ctx.stroke()
    ctx.fillStyle = '#6b3f26'; ctx.beginPath(); ctx.ellipse(55, 48, 30, 7, 0, 0, Math.PI * 2); ctx.fill()
    ctx.strokeStyle = '#8e9c94'; ctx.lineWidth = 5; ctx.lineCap = 'round'
    for (const x of [42, 58, 72]) { ctx.beginPath(); ctx.moveTo(x, 36); ctx.quadraticCurveTo(x + 8, 24, x, 12); ctx.stroke() }
  },
  // A cave level of the invented game "Emberfall": lantern light on dark rock.
  cave: (ctx, w, h) => {
    const sky = ctx.createLinearGradient(0, 0, 0, h); sky.addColorStop(0, '#0b1020'); sky.addColorStop(1, '#1b1630')
    ctx.fillStyle = sky; ctx.fillRect(0, 0, w, h)
    ctx.fillStyle = '#12101f'
    ctx.beginPath(); ctx.moveTo(0, h); ctx.lineTo(0, h * 0.62)
    for (let x = 0; x <= w; x += 40) ctx.lineTo(x, h * 0.6 + Math.sin(x / 90) * 26 + Math.cos(x / 37) * 9)
    ctx.lineTo(w, h); ctx.fill()
    ctx.beginPath(); ctx.moveTo(0, 0)
    for (let x = 0; x <= w; x += 36) ctx.lineTo(x, h * 0.14 + Math.abs(Math.sin(x / 60)) * 60)
    ctx.lineTo(w, 0); ctx.fill()
    for (const [lx, ly, r] of [[w * 0.3, h * 0.55, 190], [w * 0.72, h * 0.5, 150]]) {
      const glow = ctx.createRadialGradient(lx, ly, 4, lx, ly, r)
      glow.addColorStop(0, 'rgba(255,196,96,0.85)'); glow.addColorStop(0.35, 'rgba(255,140,60,0.28)'); glow.addColorStop(1, 'rgba(255,120,40,0)')
      ctx.fillStyle = glow; ctx.beginPath(); ctx.arc(lx, ly, r, 0, Math.PI * 2); ctx.fill()
      ctx.fillStyle = '#ffd98a'; ctx.beginPath(); ctx.arc(lx, ly, 9, 0, Math.PI * 2); ctx.fill()
      ctx.strokeStyle = '#2a2236'; ctx.lineWidth = 3; ctx.beginPath(); ctx.moveTo(lx, ly - 70); ctx.lineTo(lx, ly - 10); ctx.stroke()
    }
    ctx.fillStyle = '#0a0812'; ctx.beginPath(); ctx.ellipse(w * 0.5, h * 0.72, 14, 24, 0, 0, Math.PI * 2); ctx.fill()
    ctx.beginPath(); ctx.arc(w * 0.5, h * 0.64, 11, 0, Math.PI * 2); ctx.fill()
    ctx.fillStyle = '#f2b441'; ctx.beginPath(); ctx.arc(w * 0.5 + 9, h * 0.7, 4, 0, Math.PI * 2); ctx.fill()
  },
}

const bit = n => 2 ** n

async function makeMembers(admin, ctx) {
  const pages = {}
  for (const [username, display, status, hue] of PEOPLE) {
    const p = await (await ctx()).newPage()
    await p.goto(BASE + '/')
    const inv = must(await api(admin, 'POST', '/api/admin/invites', { max_uses: 1 }), 'invite')
    must(await api(p, 'POST', '/api/auth/register', { username, display_name: display, password: MEMBER_PASSWORD, invite_code: inv.code }), `register ${username}`)
    const letters = display.split(' ').map(w => w[0]).join('')
    must(await uploadDrawn(p, '/api/users/me/avatar', 'avatar', `${username}.png`, 256, 256, DRAW.avatar, { hue, letters }), `avatar ${username}`)
    if (status) must(await api(p, 'PUT', '/api/users/me/status', { status_text: status }), 'status')
    pages[username] = p
  }
  must(await api(admin, 'PUT', '/api/users/me/profile', { display_name: OWNER_DISPLAY }), 'owner name')
  must(await uploadDrawn(admin, '/api/users/me/avatar', 'avatar', 'maren.png', 256, 256, DRAW.avatar, { hue: 150, letters: 'MV' }), 'owner avatar')
  must(await api(admin, 'PUT', '/api/users/me/status', { status_text: 'Shipping Emberfall 0.9.2' }), 'owner status')
  return pages
}

async function makeEmoji(admin) {
  const ids = {}
  for (const name of ['gg', 'shipit', 'bug', 'coffee', 'lantern', 'fox', 'star', 'heart']) {
    const r = must(await uploadDrawn(admin, '/api/admin/emoji', 'file', `${name}.png`, 128, 128, DRAW[name], {}, { name }), `emoji ${name}`)
    ids[name] = r.id
  }
  return ids
}

async function makeLayout(admin) {
  const before = must(await api(admin, 'GET', '/api/channels'), 'channels')
  const cat = async (name, sort_order) => must(await api(admin, 'POST', '/api/admin/categories', { name, sort_order }), 'category').id
  const studio = await cat('Studio', 0)
  const dev = await cat('Development', 1)
  const community = await cat('Community', 2)
  const talks = await cat('Talks', 3)
  const ch = async (name, type, category_id, sort_order, topic = '') =>
    must(await api(admin, 'POST', '/api/admin/channels', { name, type, category_id, sort_order, topic }), `channel ${name}`).id
  const ids = {
    announcements: await ch('announcements', 'text', studio, 0, 'News, rules and the release schedule'),
    general: await ch('general', 'text', studio, 1, 'Everything about Emberfall and the studio'),
    playtests: await ch('playtests', 'text', studio, 2, 'Sign-ups and notes for playtest nights'),
    devlog: await ch('dev-log', 'text', dev, 0, 'What we built today, builds and reviews'),
    bugs: await ch('bugs', 'text', dev, 1, 'Reports with platform and save file'),
    artWip: await ch('art-wip', 'text', dev, 2, 'Work in progress from the art side'),
    screenshots: await ch('screenshots', 'text', community, 0, 'Your best moments in Emberfall'),
    offTopic: await ch('off-topic', 'text', community, 1, 'Music, food and everything else'),
    lounge: await ch('Lounge', 'voice', talks, 0),
    standup: await ch('Stand-up', 'voice', talks, 1),
    playtestNight: await ch('Playtest night', 'voice', talks, 2),
  }
  for (const c of before.categories) {
    for (const x of c.channels ?? []) must(await api(admin, 'DELETE', `/api/admin/channels/${x.id}`), 'delete default channel')
    must(await api(admin, 'DELETE', `/api/admin/categories/${c.id}`), 'delete default category')
  }
  for (const x of before.uncategorized) must(await api(admin, 'DELETE', `/api/admin/channels/${x.id}`), 'delete default channel')
  return ids
}

async function makeRoles(admin) {
  const role = async (name, color, permissions, hoist) =>
    must(await api(admin, 'POST', '/api/admin/roles', { name, color, permissions, hoist }), `role ${name}`)
  await role('Devs', 0x2da771, bit(11) + bit(13), true)
  await role('Artists', 0x9b7ede, 0, true)
  const list = must(await api(admin, 'GET', '/api/roles'), 'roles')
  const all = list.roles ?? list
  const idOf = n => all.find(r => r.name === n).id
  const members = must(await api(admin, 'GET', '/api/members'), 'members')
  const users = members.members ?? members
  const uid = n => users.find(u => u.username === n).id
  for (const [who, roleName] of [['ines', 'Moderator'], ['theo', 'Devs'], ['priya', 'Devs'], ['kai', 'Artists']]) {
    must(await api(admin, 'PUT', `/api/admin/users/${uid(who)}/roles`, { role_ids: [idOf(roleName)] }), 'assign')
  }
}

async function makeCommunityTexts(admin, ids) {
  must(await api(admin, 'PUT', '/api/admin/onboarding', {
    community_name: 'Lantern Fox Studio',
    welcome_text: 'Welcome to the Lantern Fox community. We are a small studio making **Emberfall**, a lantern-lit platformer. Say hello in #general and join a playtest when you can.',
    rules_text: '1. Be kind. We are all here for the same game.\n2. Spoilers go behind spoiler tags.\n3. Bug reports belong in #bugs.\n4. No advertising.',
    start_channel_id: ids.general, require_reacceptance: false,
  }), 'onboarding')
}

export async function seed(browser) {
  const ctx = () => browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 900 } })
  const admin = await (await ctx()).newPage()
  await login(admin, ADMIN_USER, ADMIN_PASSWORD)

  const pages = await makeMembers(admin, ctx)
  const emoji = await makeEmoji(admin)
  const ids = await makeLayout(admin)
  await makeRoles(admin)
  await makeCommunityTexts(admin, ids)
  // Members accept the rules before they may post, like anyone joining.
  for (const page of Object.values(pages)) {
    const state = must(await api(page, 'GET', '/api/onboarding'), 'onboarding')
    if (state.rules_pending) must(await api(page, 'POST', '/api/onboarding/accept', { version: state.rules_version }), 'accept rules')
  }
  await converse(admin, pages, ids, emoji)
  spreadOverADay()
  return { admin, pages, ids }
}

async function converse(admin, pages, ids, emoji) {
  const e = name => `<:${name}:${emoji[name]}>`
  const who = n => (n === 'maren' ? admin : pages[n])
  const say = async (name, channel, content, extra = {}) =>
    must(await api(who(name), 'POST', `/api/channels/${channel}/messages`, { content, ...extra }), 'message')
  const react = async (name, msg, emoji) =>
    must(await api(who(name), 'POST', `/api/messages/${msg.id}/reactions`, { emoji }), 'react')

  // #announcements: the pinned rules and the release schedule
  const rules = await say('maren', ids.announcements, `**Welcome to the Lantern Fox community.** ${e('fox')} Emberfall is our lantern-lit platformer and this is where we share news, playtests and the odd dev diary.\n\n1. Be kind, we are all here for the same game.\n2. Spoilers go behind ||spoiler tags||.\n3. Bug reports belong in #bugs, with your platform.\n4. No advertising, please.`)
  const schedule = await say('maren', ids.announcements, '**Release schedule**\n- Playtest 3: Saturday, 18:00 UTC in the *Playtest night* Talk\n- Patch 0.9.2: next Tuesday\n- Store page demo: end of the month')
  await say('ines', ids.announcements, 'Reminder from the mod team: please attach your save file when you report a bug. It saves us a day of guessing. Thank you all!')
  for (const m of [rules, schedule]) must(await api(admin, 'PUT', `/api/channels/${ids.announcements}/messages/${m.id}/pin`), 'pin')

  // #dev-log: the main chat picture (Markdown, code, thread, mention, reactions)
  const light = await say('theo', ids.devlog, `Pushed the lantern lighting rewrite. Dynamic lights are batched now, so the cave level went from **9.1 ms** to **5.4 ms** per frame ${e('shipit')}`)
  await say('priya', ids.devlog, 'Nice! Does it still flicker on the old integrated GPUs?')
  await say('theo', ids.devlog, 'Not any more. This is the part that mattered, one uniform block per batch instead of per light:\n```rust\nfor batch in lights.chunks(MAX_LIGHTS_PER_PASS) {\n    pass.set_uniforms(&LightBlock::from(batch));\n    pass.draw_indexed(0..quad.index_count, 0, 0..1);\n}\n```')
  await say('kai', ids.devlog, `The cave looks so much warmer now. I will post the new rock textures in #art-wip tonight ${e('gg')}`)
  const save = await say('priya', ids.devlog, '@maren the save-corruption bug from #bugs is reproducible on my machine now. Details in the thread, it only happens when the game is closed during autosave.')
  await say('maren', ids.devlog, 'Thanks Priya, that explains the two reports from last week. Can you check whether the temp file is still there after the crash?', { parent_id: save.id })
  await say('priya', ids.devlog, 'Yes, `save.tmp` survives and is complete. So we can recover from it on the next start instead of showing the corrupt file.', { parent_id: save.id })
  await say('theo', ids.devlog, 'I can write the recovery path tomorrow morning. It is about 30 lines.', { parent_id: save.id })
  await say('maren', ids.devlog, `Perfect, that goes into 0.9.2. ${e('bug')} squashed`, { parent_id: save.id })
  await react('maren', light, '🚀'); await react('priya', light, '🚀'); await react('kai', light, '🚀'); await react('ines', light, '🔥'); await react('leo', light, '🎉')
  await react('theo', save, '👀'); await react('kai', save, '👍')

  // #screenshots: a picture from the game
  must(await uploadDrawn(pages.kai, `/api/channels/${ids.screenshots}/upload`, 'file', 'cave-level-lanterns.png', 960, 540, DRAW.cave, {}, { content: 'Cave level with the new lantern lighting. The glow finally reads as warm light and not as an orange blob.' }), 'screenshot')

  // #general: some days of chat, then the admin reads, then newer messages (the unread divider)
  await say('leo', ids.general, `Hi everyone! Just finished the demo, the lantern puzzle in the second cave is brilliant. ${e('lantern')}`)
  await say('noor', ids.general, `Same, I got stuck on it for an hour and loved every minute ${e('coffee')}`)
  await say('maren', ids.general, 'Thank you both. That puzzle took us three redesigns, so this makes my week.')
  await say('sam', ids.general, 'Is the next playtest on controller as well? I only have the pad here.')
  const pad = await say('ines', ids.general, 'Yes, controller works. Keyboard and pad can even be mixed in co-op.')
  await react('sam', pad, '🎮')
  must(await api(admin, 'POST', `/api/channels/${ids.general}/read`, { message_id: pad.id }), 'read')
  await say('noor', ids.general, 'Great, then I will bring my brother as player two.')
  await say('leo', ids.general, `@maren will the demo get the new lighting as well? ${e('gg')}`)
  await say('theo', ids.general, 'Only after 0.9.2, we want one more week of testing on it.')
  await say('sam', ids.general, 'Fair. Looking forward to Saturday!')

  // #playtests: a poll with votes
  await say('maren', ids.playtests, 'Playtest 3 is coming. We need to pick the day, so please vote below. The session lasts about two hours and we play the whole second world.')
  const poll = must(await api(pages.ines, 'POST', `/api/channels/${ids.playtests}/polls`, {
    question: 'Which day for the next playtest?', options: ['Friday evening', 'Saturday afternoon', 'Sunday evening'], duration: '3d',
  }), 'poll')
  const opt = poll.poll.options
  for (const [name, pick] of [['sam', 1], ['leo', 1], ['noor', 1], ['priya', 0], ['theo', 1], ['kai', 2]]) {
    must(await api(who(name), 'PUT', `/api/channels/${ids.playtests}/messages/${poll.id}/poll/votes`, { option_ids: [opt[pick].id] }), 'vote')
  }

  // #bugs, #art-wip, #off-topic: a little life
  await say('leo', ids.bugs, '**Windows 11**, controller: the second lantern does not light when I jump while pressing interact. Happens about every third try.')
  await say('ines', ids.bugs, 'Thanks Leo, I could reproduce it. Moved to the tracker as EMB-212.')
  await say('kai', ids.artWip, 'New rock texture set for the cave: three variants, tileable. Feedback on the colour welcome, I pushed the warm tones a bit.')
  await say('theo', ids.artWip, 'They look great under the new lights. Could the darkest variant be 10% lighter? Right now it swallows the lantern glow.')
  await say('noor', ids.offTopic, 'What do you all listen to while working? I need new focus music.')
  await say('priya', ids.offTopic, 'Lo-fi with the occasional synthwave. Today it is an album of rain sounds, which is surprisingly good for debugging.')

  await webhooks(admin, ids)
}

async function webhooks(admin, ids) {
  const hook = must(await api(admin, 'POST', `/api/admin/channels/${ids.devlog}/webhooks`, { name: 'Build bot' }), 'webhook')
  const url = hook.url ?? hook.webhook?.url
  if (url) {
    const status = await admin.evaluate(async (u) => {
      const res = await fetch(u, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
        username: 'Build bot',
        embeds: [{ title: 'Build #482 passed', description: 'main · 41 tests · 3 min 12 s · lantern lighting rewrite', color: 0x2da771 }],
      }) })
      return res.status
    }, url)
    if (status >= 300) throw new Error(`webhook execute failed: ${status}`)
  }
  const out = await api(admin, 'POST', `/api/admin/channels/${ids.devlog}/outgoing-webhooks`, {
    name: 'Changelog site', url: 'https://hooks.example.com/changelog', events: ['MESSAGE_CREATE'],
  })
  if (out.status >= 300) console.warn('outgoing webhook not created:', out.status, JSON.stringify(out.json))
}

// Everything was written within seconds; give it the rhythm of one working day
// (yesterday, 09:40 onwards) so times and day dividers look natural.
function spreadOverADay() {
  const sql = `
    UPDATE messages m SET created_at = t.ts, updated_at = t.ts FROM (
      SELECT id, date_trunc('day', now()) - interval '1 day' + interval '9 hours 40 minutes'
        + row_number() OVER (ORDER BY created_at, number) * interval '3 minutes'
        + (abs(hashtext(id::text)) % 3) * interval '20 seconds' AS ts
      FROM messages) t WHERE m.id = t.id;
    -- Everyone joined a few days ago, so unread counts start there. The Owner has
    -- read the announcements and #dev-log, and #general up to Ines' answer about
    -- the controller; the other channels are unread.
    UPDATE users SET created_at = now() - interval '3 days';
    DELETE FROM channel_reads WHERE user_id = (SELECT id FROM users WHERE username = :'owner');
    INSERT INTO channel_reads (user_id, channel_id, last_read_at)
      SELECT u.id, c.id, CASE WHEN c.name = 'general'
        THEN (SELECT created_at FROM messages WHERE channel_id = c.id AND content LIKE 'Yes, controller works%')
        ELSE now() END
      FROM users u, channels c WHERE u.username = :'owner' AND c.name IN ('announcements', 'dev-log', 'general');`
  const project = process.env.SHOTS_COMPOSE_PROJECT || 'mnema-shots'
  const file = new URL('../../e2e/docker-compose.yml', import.meta.url).pathname
  execFileSync('docker', ['compose', '-p', project, '-f', file, 'exec', '-T', 'postgres', 'psql', '-U', 'mnema', '-d', 'mnema_e2e', '-v', 'ON_ERROR_STOP=1', '-v', `owner=${ADMIN_USER}`], { input: sql, stdio: ['pipe', 'inherit', 'inherit'] })
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const browser = await chromium.launch()
  await seed(browser)
  await browser.close()
  console.log('seeded')
}
