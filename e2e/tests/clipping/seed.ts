import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { createHash, randomBytes } from 'node:crypto'
import { deflateSync } from 'node:zlib'
import type { Browser, Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId } from '../helpers'

// The world the clipping sweep looks at: one of each kind of content, every
// text as long as the server allows where length is the risk. Seeded once per
// server (cached in a file next to the test results, because Playwright starts
// a new worker after a failed test and the sweep's tests fail by design while
// there are findings).

export const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
export const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''

export interface World {
  suffix: string
  chat: { id: string; slug: string; name: string }
  longName: { id: string; slug: string; name: string }
  talk: { id: string; slug: string; name: string }
  forum: { id: string; slug: string }
  wikiSlug: string
  eventId: string
  dmId: string
  groupDmId: string
  member: { id: string; username: string; password: string }
  blockedId: string
  imageMessageText: string
  lastMessageText: string
}

const LONG_WORD = 'Pneumonoultramicroscopicsilicovolcanoconiosis'.repeat(3)
const LONG_LINK = `https://docs.example.com/guides/${'very-long-path-segment-'.repeat(8)}end?query=${'value'.repeat(12)}`
const CODE = [
  '```ts',
  `export function averyLongFunctionNameThatKeepsGoing(parameterNumberOne: string, parameterNumberTwo: number): Promise<Map<string, ReadonlyArray<number>>> { return fetchEverything(parameterNumberOne, parameterNumberTwo) }`,
  '```',
].join('\n')

// A gradient PNG of the given size: real pixels, so the image lays out like a photo.
function png(width: number, height: number): Buffer {
  const table = Array.from({ length: 256 }, (_, n) => {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    return c >>> 0
  })
  const crc = (data: Buffer) => {
    let c = 0xffffffff
    for (const byte of data) c = (table[(c ^ byte) & 0xff] ?? 0) ^ (c >>> 8)
    return (c ^ 0xffffffff) >>> 0
  }
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4)
    length.writeUInt32BE(data.length)
    const body = Buffer.concat([Buffer.from(type), data])
    const sum = Buffer.alloc(4)
    sum.writeUInt32BE(crc(body))
    return Buffer.concat([length, body, sum])
  }
  const rows = Buffer.alloc((width * 3 + 1) * height)
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const at = y * (width * 3 + 1) + 1 + x * 3
      rows[at] = (x * 255) / width
      rows[at + 1] = 120
      rows[at + 2] = (y * 255) / height
    }
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header[8] = 8
  header[9] = 2
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0))])
}

export const IMAGE = png(1600, 900)

async function ok(page: Page, method: string, path: string, body?: unknown, expected = [200, 201, 204]) {
  const res = await apiFetch(page, method, path, body)
  if (!expected.includes(res.status)) throw new Error(`${method} ${path} answered ${res.status}: ${JSON.stringify(res.json).slice(0, 300)}`)
  return res
}

export async function signInApi(page: Page, username = ADMIN_USER, password = ADMIN_PASSWORD) {
  if (page.url() === 'about:blank') await page.goto('/')
  await ok(page, 'POST', '/api/auth/login', { username, password })
}

/** Posts a message with one attachment: the upload itself creates the message. */
async function upload(page: Page, channelId: string, content: string, name: string, type: string, bytes: Buffer): Promise<string> {
  const body = [...bytes]
  const result = await page.evaluate(async ({ channelId, content, name, type, body }) => {
    const form = new FormData()
    // The fields come before the file: the server reads the form as a stream.
    form.append('content', content)
    form.append('file', new Blob([new Uint8Array(body)], { type }), name)
    const res = await fetch(`/api/channels/${channelId}/upload`, { method: 'POST', body: form, credentials: 'same-origin' })
    return { status: res.status, text: await res.text() }
  }, { channelId, content, name, type, body })
  if (result.status !== 201) throw new Error(`upload answered ${result.status}: ${result.text.slice(0, 200)}`)
  const id: unknown = (JSON.parse(result.text) as Record<string, unknown>)['id']
  if (typeof id !== 'string') throw new Error('upload without id')
  return id
}

async function channel(page: Page, name: string, type: string, topic = '') {
  const res = await ok(page, 'POST', '/api/admin/channels', { name, type, ...(topic ? { topic } : {}) })
  return { id: responseString(res, 'id'), slug: responseString(res, 'slug'), name }
}

async function post(page: Page, channelId: string, content: string, extra: Record<string, unknown> = {}): Promise<string> {
  return responseString(await ok(page, 'POST', `/api/channels/${channelId}/messages`, { content, ...extra }), 'id')
}

/** A member of its own, registered through an invite from a context with its own client address. */
async function registerMember(browser: Browser, admin: Page, username: string, password: string) {
  const invite = await ok(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto('/')
  const res = await ok(page, 'POST', '/api/auth/register', { username, password, invite_code: responseString(invite, 'code') })
  // Registration answers with { user }, /api/auth/me with the user itself.
  return { ctx, page, id: responseUserId({ ...res, json: (res.json as { user?: unknown }).user }) }
}

async function seedChat(admin: Page, member: Page, memberName: string, suffix: string) {
  const chat = await channel(admin, `clip-chat-${suffix}`, 'text', `A topic that keeps going so the header has to decide what to do with it: ${'and more '.repeat(12)}`)
  const first = await post(admin, chat.id, `The first message, with **bold**, _italics_, \`inline code that is quite long indeed\` and a mention of @${memberName}.`)
  await post(admin, chat.id, LONG_WORD)
  await post(admin, chat.id, `A long link: ${LONG_LINK}`)
  await post(admin, chat.id, CODE)
  await post(admin, chat.id, `> A quote that runs over more than one line, ${'quoted words '.repeat(14)}\n\nand a list:\n- one\n- two with ${LONG_WORD.slice(0, 40)}`)
  const imageMessageText = `A picture ${suffix}`
  await upload(admin, chat.id, imageMessageText, 'landscape-photo-with-a-rather-long-file-name.png', 'image/png', IMAGE)
  await upload(admin, chat.id, 'A file', `quarterly-report-final-version-reviewed-${'x'.repeat(40)}.txt`, 'text/plain', Buffer.from('plain text file\n'))
  await post(admin, chat.id, `A reply to the first message ${'with words '.repeat(6)}`, { reply_to_id: first })
  await post(admin, chat.id, 'A reply in a thread with a long word to wrap', { parent_id: first })
  await post(member, chat.id, `@${ADMIN_USER} a mention from the member, ${'with enough text to wrap '.repeat(3)}`)
  await ok(admin, 'POST', `/api/channels/${chat.id}/polls`, {
    question: `Which of these options is the right one for the long poll question ${'that wraps '.repeat(5)}?`,
    // 55 characters is the longest option (internal/poll).
    options: ['A short one', `A long option that keeps going ${'and going '.repeat(5)}`.slice(0, 55), LONG_WORD.slice(0, 55)],
    multiple: true,
  })
  const pinned = await post(admin, chat.id, 'A message that gets pinned, which leaves a notice in the channel')
  await ok(admin, 'PUT', `/api/channels/${chat.id}/messages/${pinned}/pin`)
  return { chat, first, imageMessageText }
}

async function seedEmbed(admin: Page, channelId: string) {
  const hook = await ok(admin, 'POST', `/api/admin/channels/${channelId}/webhooks`, { name: 'Release pipeline with a long name' })
  const sent = await admin.request.post(responseString(hook, 'url'), {
    data: {
      content: 'An embed from a webhook',
      username: 'Continuous Integration Bot',
      embeds: [{
        title: `Build ${'pipeline '.repeat(6)}finished`,
        description: `All checks passed. ${'Details follow in a description that is long enough to wrap several times. '.repeat(3)}${LONG_WORD.slice(0, 50)}`,
        url: LONG_LINK,
        color: 3066993,
        author: { name: 'An author with a long display name for the embed' },
        fields: [
          { name: 'Tests', value: '812 passed', inline: true },
          { name: 'Duration of the whole run', value: '12 minutes 41 seconds', inline: true },
          { name: 'A field without inline', value: LONG_LINK, inline: false },
        ],
        footer: { text: `Footer text ${'that is long '.repeat(6)}` },
      }],
    },
  })
  if (sent.status() !== 204) throw new Error(`webhook answered ${sent.status()}`)
}

async function seedElsewhere(admin: Page, member: Page, other: string, adminId: string, suffix: string) {
  const longName = await channel(admin, `a-channel-with-the-longest-name-the-server-allows-${suffix}`.slice(0, 64), 'text')
  const talk = await channel(admin, `A Talk with a long name for the sweep ${suffix}`.slice(0, 64), 'voice')
  const forum = await channel(admin, `clip-forum-${suffix}`, 'forum')
  for (let i = 0; i < 3; i++) await ok(admin, 'POST', `/api/channels/${forum.id}/posts`, { title: `Forum post ${i} with a title long enough to need two lines ${'or more '.repeat(4)}`, content: `Body ${i}. ${LONG_WORD}` })
  const wiki = await ok(admin, 'POST', '/api/wiki/pages', { title: `A wiki page with a long title for the sweep ${suffix}`, content: `# Heading\n\n${'Paragraph text. '.repeat(40)}\n\n${CODE}\n\n| Column one | Column two with a long header | Three |\n|---|---|---|\n| ${LONG_WORD} | b | c |\n\n${LONG_LINK}` })
  const start = new Date(Date.now() + 3 * 86_400_000).toISOString()
  const event = await ok(admin, 'POST', '/api/events', { title: `Community evening with a long title ${'and more words '.repeat(3)}`.slice(0, 100), description: `${'Description text. '.repeat(20)}${LONG_LINK}`, start_time: start, location: `A location with a long address, ${'street '.repeat(5)}` })
  const eventId = responseString(event, 'id')
  await ok(member, 'POST', `/api/events/${eventId}/comments`, { content: `A comment ${'with words '.repeat(10)}${LONG_WORD.slice(0, 50)}` })
  const dm = await ok(member, 'POST', '/api/dms', { user_ids: [adminId] })
  const dmId = responseString(dm, 'id')
  await post(member, dmId, `Hello in a direct message, ${'with a long line '.repeat(6)}`)
  await post(admin, dmId, LONG_WORD)
  const group = await ok(member, 'POST', '/api/dms', { user_ids: [adminId, other] })
  const groupDmId = responseString(group, 'id')
  await post(member, groupDmId, 'Hello group')
  return { longName, talk, forum: { id: forum.id, slug: forum.slug }, wikiSlug: responseString(wiki, 'slug'), eventId, dmId, groupDmId }
}

function cachePath(baseURL: string, outputDir: string): string {
  const key = createHash('sha256').update(`${baseURL} ${ADMIN_PASSWORD}`).digest('hex').slice(0, 16)
  return join(outputDir, `clip-world-${key}.json`)
}

/** Seeds the world once per server and returns it; later workers read the cached ids. */
export async function seedWorld(browser: Browser, baseURL: string, outputDir: string): Promise<World> {
  mkdirSync(outputDir, { recursive: true })
  const cache = cachePath(baseURL, outputDir)
  if (existsSync(cache)) return JSON.parse(readFileSync(cache, 'utf8')) as World
  const suffix = Date.now().toString(36)
  const admin = await (await browser.newContext()).newPage()
  await signInApi(admin)
  const adminId = responseUserId(await apiFetch(admin, 'GET', '/api/auth/me'))
  const password = `pw-${suffix}-${randomBytes(8).toString('hex')}`
  const member = await registerMember(browser, admin, `clip_member_${suffix}`, password)
  const other = await registerMember(browser, admin, `clip_blocked_${suffix}`, password)
  const { chat, imageMessageText } = await seedChat(admin, member.page, `clip_member_${suffix}`, suffix)
  await seedEmbed(admin, chat.id)
  // A forwarded message, and a message of a member the Owner has blocked (the fold).
  const source = await post(member.page, chat.id, 'A message worth forwarding to the same channel')
  await ok(member.page, 'POST', `/api/messages/${source}/forward`, { channel_id: chat.id, comment: `Forwarded with a comment ${'that is long '.repeat(5)}` })
  await post(other.page, chat.id, 'A message from a blocked member')
  await ok(admin, 'PUT', `/api/me/blocks/${other.id}`)
  const lastMessageText = `The last message ${suffix}`
  const last = await post(admin, chat.id, lastMessageText)
  await ok(admin, 'PUT', `/api/me/saved/${last}`)
  await ok(admin, 'POST', '/api/me/reminders', { message_id: last, remind_at: new Date(Date.now() + 86_400_000).toISOString() })
  const elsewhere = await seedElsewhere(admin, member.page, other.id, adminId, suffix)
  const world: World = { suffix, chat, ...elsewhere, member: { id: member.id, username: `clip_member_${suffix}`, password }, blockedId: other.id, imageMessageText, lastMessageText }
  for (const ctx of [member.ctx, other.ctx, admin.context()]) await ctx.close()
  writeFileSync(cache, JSON.stringify(world))
  return world
}

/**
 * Takes the world away again: the other specs share this server and count
 * channels, unread messages, mentions and events. The next worker seeds anew.
 */
export async function removeWorld(browser: Browser, baseURL: string, outputDir: string): Promise<void> {
  const cache = cachePath(baseURL, outputDir)
  if (!existsSync(cache)) return
  const world = JSON.parse(readFileSync(cache, 'utf8')) as World
  rmSync(cache)
  const ctx = await browser.newContext()
  const admin = await ctx.newPage()
  await signInApi(admin)
  const statuses: number[] = []
  for (const id of [world.chat.id, world.longName.id, world.talk.id, world.forum.id]) statuses.push((await apiFetch(admin, 'DELETE', `/api/admin/channels/${id}`)).status)
  for (const id of [world.dmId, world.groupDmId]) statuses.push((await apiFetch(admin, 'PUT', `/api/dms/${id}/hidden`, { hidden: true })).status)
  statuses.push((await apiFetch(admin, 'DELETE', `/api/wiki/pages/${world.wikiSlug}`)).status)
  statuses.push((await apiFetch(admin, 'POST', `/api/events/${world.eventId}/cancel`, {})).status)
  statuses.push((await apiFetch(admin, 'DELETE', `/api/me/blocks/${world.blockedId}`)).status)
  console.log(`[clip] removed the seeded world: ${statuses.join(' ')}`)
  await ctx.close()
}
