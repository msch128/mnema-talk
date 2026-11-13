import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'
import { actionRow, button, RestClient, RestError, ValidationError, type RateLimitInfo, type RestOptions } from '../src/index.ts'
import { sendJson, startHttpServer, type Handler, type TestHttpServer } from './helpers/httpServer.ts'

let server: TestHttpServer | undefined

afterEach(async () => {
  await server?.close()
  server = undefined
})

async function client(handler: Handler, extra: Partial<RestOptions> = {}): Promise<RestClient> {
  server = await startHttpServer(handler)
  return new RestClient({ baseUrl: `${server.url}/`, token: 'mnb_secret', ...extra })
}

const ok: Handler = (req, res) => {
  if (req.method === 'DELETE' || req.method === 'PUT' && req.url.includes('/reactions/')) return void res.writeHead(204).end()
  sendJson(res, 200, { id: 'x', echo: req.method })
}

describe('request shapes', () => {
  test('sends the bot token, JSON and the versioned base path', async () => {
    const rest = await client(ok)
    await rest.createMessage('ch-1', { content: 'build **passed**', embeds: [{ title: 'Run 42', color: 65280 }] })
    const r = server!.requests[0]!
    assert.equal(r.method, 'POST')
    assert.equal(r.url, '/api/bot/v1/channels/ch-1/messages')
    assert.equal(r.headers.authorization, 'Bot mnb_secret')
    assert.equal(r.headers['content-type'], 'application/json')
    assert.match(String(r.headers['user-agent']), /^mnema-talk-sdk\//)
    assert.deepEqual(r.json(), { content: 'build **passed**', embeds: [{ title: 'Run 42', color: 65280 }] })
  })

  test('maps each route to its method and path', async () => {
    const rest = await client(ok)
    await rest.getMe()
    await rest.listChannels()
    await rest.listMessages('c', { limit: 10, before: 'm0' })
    await rest.getMessage('c', 'm')
    await rest.editMessage('c', 'm', { components: [] })
    assert.equal(await rest.deleteMessage('c', 'm'), undefined)
    await rest.addReaction('c', 'm', '👍')
    await rest.removeReaction('c', 'm', '<:party:abc>')
    await rest.listMembers()
    await rest.getMember('u')
    await rest.listCommands()
    await rest.deleteCommand('cmd')
    await rest.getGateway()
    assert.deepEqual(
      server!.requests.map((r) => `${r.method} ${r.url}`),
      [
        'GET /api/bot/v1/users/@me',
        'GET /api/bot/v1/channels',
        'GET /api/bot/v1/channels/c/messages?limit=10&before=m0',
        'GET /api/bot/v1/channels/c/messages/m',
        'PATCH /api/bot/v1/channels/c/messages/m',
        'DELETE /api/bot/v1/channels/c/messages/m',
        'PUT /api/bot/v1/channels/c/messages/m/reactions/%F0%9F%91%8D/@me',
        'DELETE /api/bot/v1/channels/c/messages/m/reactions/%3C%3Aparty%3Aabc%3E/@me',
        'GET /api/bot/v1/members',
        'GET /api/bot/v1/members/u',
        'GET /api/bot/v1/commands',
        'DELETE /api/bot/v1/commands/cmd',
        'GET /api/bot/v1/gateway',
      ],
    )
    assert.deepEqual(server!.requests[4]!.json(), { components: [] })
  })

  test('registers commands with an options list and answers interactions on their routes', async () => {
    const rest = await client(ok)
    await rest.setCommands([{ name: 'ping', description: 'Replies with pong' }])
    await rest.createInteractionResponse('i1', 'tok/en', { type: 4, data: { content: 'pong', flags: 64 } })
    await rest.createFollowup('i1', 'tok', { content: 'more' })
    await rest.editOriginalResponse('i1', 'tok', { content: 'edited' })
    await rest.deleteOriginalResponse('i1', 'tok')
    const [put, cb, fu, patch, del] = server!.requests
    assert.deepEqual(put!.json(), [{ name: 'ping', description: 'Replies with pong', options: [] }])
    assert.equal(cb!.url, '/api/bot/v1/interactions/i1/tok%2Fen/callback')
    assert.deepEqual(cb!.json(), { type: 4, data: { content: 'pong', flags: 64 } })
    assert.equal(fu!.url, '/api/bot/v1/interactions/i1/tok/followups')
    assert.equal(`${patch!.method} ${patch!.url}`, 'PATCH /api/bot/v1/interactions/i1/tok/messages/@original')
    assert.equal(`${del!.method} ${del!.url}`, 'DELETE /api/bot/v1/interactions/i1/tok/messages/@original')
  })

  test('uploads a file as multipart with its name and caption', async () => {
    const rest = await client(ok)
    await rest.uploadFile('c', { file: Buffer.from('hello'), name: 'note.txt', contentType: 'text/plain', content: 'see', reply_to_id: 'm1' })
    const r = server!.requests[0]!
    assert.match(String(r.headers['content-type']), /^multipart\/form-data; boundary=/)
    const text = r.body.toString('utf8')
    assert.match(text, /name="file"; filename="note.txt"/)
    assert.match(text, /Content-Type: text\/plain\r\n\r\nhello/)
    assert.match(text, /name="content"\r\n\r\nsee/)
    assert.match(text, /name="reply_to_id"\r\n\r\nm1/)
  })

  test('checks components before sending anything', async () => {
    const rest = await client(ok)
    const row = actionRow(button({ customId: 'a', label: 'A' }))
    const sixRows = Array.from({ length: 6 }, () => row)
    assert.throws(() => rest.createMessage('c', { components: sixRows }), ValidationError)
    assert.throws(() => rest.createMessage('c', { components: [row, row] }), /used twice/)
    assert.equal(server!.requests.length, 0)
  })
})

describe('errors', () => {
  test('an error answer becomes a RestError with status, code, message and body', async () => {
    const rest = await client((_req, res) => sendJson(res, 404, { message: 'Unknown Channel', code: 10003 }))
    const err = await rest.getMessage('c', 'm').catch((e: unknown) => e)
    assert.ok(err instanceof RestError)
    assert.equal(err.status, 404)
    assert.equal(err.code, 10003)
    assert.equal(err.message, 'Unknown Channel')
    assert.deepEqual(err.body, { message: 'Unknown Channel', code: 10003 })
    assert.equal(err.method, 'GET')
    assert.equal(err.path, '/channels/c/messages/m')
  })

  test('a body that is not JSON is kept as text with code 0', async () => {
    const rest = await client((_req, res) => res.writeHead(500, { 'Content-Type': 'text/plain' }).end('boom'))
    const err = (await rest.createMessage('c', 'x').catch((e: unknown) => e)) as RestError
    assert.equal(err.status, 500)
    assert.equal(err.code, 0)
    assert.equal(err.body, 'boom')
  })

  test('a request that takes too long is aborted', async () => {
    const rest = await client(() => {}, { timeoutMs: 50 })
    await assert.rejects(rest.getMe(), (e: Error) => e.name === 'TimeoutError')
  })

  test('a GET that meets 503 is tried again', async () => {
    let n = 0
    const rest = await client((_req, res) => (n++ === 0 ? sendJson(res, 503, { message: 'busy', code: 0 }) : sendJson(res, 200, [])))
    assert.deepEqual(await rest.listMembers(), [])
    assert.equal(n, 2)
  })
})

describe('rate limits', () => {
  test('a 429 waits retry_after and sends the request again', async () => {
    let n = 0
    const seen: RateLimitInfo[] = []
    const rest = await client(
      (_req, res) => {
        if (n++ === 0) return sendJson(res, 429, { message: 'You are being rate limited.', retry_after: 0.15, global: false }, { 'Retry-After': '1', 'X-RateLimit-Scope': 'user' })
        sendJson(res, 201, { id: 'm1' })
      },
      { onRateLimit: (i) => seen.push(i) },
    )
    const start = Date.now()
    const msg = await rest.createMessage('c', 'hi')
    assert.equal(msg.id, 'm1')
    assert.ok(Date.now() - start >= 140)
    assert.deepEqual(seen, [
      { method: 'POST', path: '/channels/c/messages', bucket: 'post-message:c', retryAfter: 0.15, global: false, scope: 'user', attempt: 1, willRetry: true },
    ])
  })

  test('without a body the Retry-After header decides', async () => {
    let n = 0
    const rest = await client((_req, res) => (n++ === 0 ? res.writeHead(429, { 'Retry-After': '1' }).end() : sendJson(res, 200, { id: 'me' })))
    const start = Date.now()
    await rest.getMe()
    assert.ok(Date.now() - start >= 950)
  })

  test('a global 429 holds back every route', async () => {
    let first = true
    const rest = await client((req, res) => {
      if (first) {
        first = false
        return sendJson(res, 429, { message: 'You are being rate limited.', retry_after: 0.2, global: true }, { 'X-RateLimit-Global': 'true' })
      }
      sendJson(res, 200, { url: req.url })
    })
    const start = Date.now()
    const a = rest.getMe()
    await new Promise((r) => setTimeout(r, 30))
    await rest.listChannels()
    assert.ok(Date.now() - start >= 190, 'the other route waited for the global pause')
    await a
  })

  test('after the last retry the 429 is thrown', async () => {
    const rest = await client((_req, res) => sendJson(res, 429, { message: 'You are being rate limited.', retry_after: 0.01, global: false }), { maxRetries: 2 })
    const err = (await rest.getMe().catch((e: unknown) => e)) as RestError
    assert.equal(err.status, 429)
    assert.equal(err.retryAfter, 0.01)
    assert.equal(err.global, false)
    assert.equal(server!.requests.length, 3)
  })

  test('the sixth message in five seconds waits locally; other channels do not', async () => {
    const rest = await client(ok)
    for (let i = 0; i < 5; i++) await rest.createMessage('busy', `m${i}`)
    await rest.createMessage('quiet', 'other channel')
    const abort = new AbortController()
    const sixth = rest.request('POST', '/channels/busy/messages', { body: { content: 'm5' }, signal: abort.signal })
    await new Promise((r) => setTimeout(r, 100))
    assert.equal(server!.requests.length, 6)
    abort.abort()
    await assert.rejects(sixth)
  })
})
