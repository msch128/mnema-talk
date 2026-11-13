// What a broken proxy or a hostile server can send must not end the bot's
// process, spin a timer or stall every route.
import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'
import { encodeManifest, Gateway, Intents, RestClient, RestError, ValidationError, type Manifest } from '../src/index.ts'
import { heartbeatInterval } from '../src/gateway/gateway.ts'
import { startFakeGateway, type FakeGateway } from './helpers/fakeGateway.ts'
import { sendJson, startHttpServer, type TestHttpServer } from './helpers/httpServer.ts'

let fake: FakeGateway | undefined
let gw: Gateway | undefined
let server: TestHttpServer | undefined

afterEach(async () => {
  await gw?.close()
  await fake?.close()
  await server?.close()
  gw = fake = server = undefined
})

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))

function gateway(url: string): Gateway {
  gw = new Gateway({
    token: 'mnb_test',
    intents: [Intents.GuildMessages],
    url,
    reconnect: { minDelayMs: 10, maxDelayMs: 40, rateLimitedDelayMs: 300 },
    random: () => 0,
  })
  gw.on('error', () => {})
  return gw
}

describe('malformed frames from the gateway', () => {
  test('a null frame and frames without an op are ignored, the session goes on', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url)
    await g.connect()
    const sock = fake.server.sockets[0]!
    sock.send(null)
    sock.send(42)
    sock.send({ d: {} })
    sock.send({ op: 0, t: 'MESSAGE_CREATE', s: null, d: null })
    fake.dispatch(sock, 'MESSAGE_CREATE', { id: 'm1' })
    await wait(50)
    assert.equal(fake.server.sockets.length, 1)
  })

  test('a READY without a payload drops the connection instead of throwing, and the bot identifies again', async () => {
    let identifies = 0
    fake = await startFakeGateway(undefined, {
      onIdentify: (s) => {
        identifies++
        if (identifies === 1) s.send({ op: 0, t: 'READY', s: 1, d: null })
        else fake!.ready(s)
      },
    })
    const ready = await gateway(fake.url).connect()
    assert.equal(ready.session_id, 'session-1')
    assert.equal(identifies, 2)
  })

  test('the HELLO interval is clamped: 0 does not beat in a loop, a day is cut to five minutes', () => {
    assert.equal(heartbeatInterval({ heartbeat_interval: 0 }), 1000)
    assert.equal(heartbeatInterval({ heartbeat_interval: 86_400_000 }), 300_000)
    assert.equal(heartbeatInterval({ heartbeat_interval: 41_250 }), 41_250)
    assert.equal(heartbeatInterval(null), undefined)
    assert.equal(heartbeatInterval({ heartbeat_interval: 'soon' }), undefined)
  })

  test('a HELLO with interval 0 sends no burst of heartbeats', async () => {
    fake = await startFakeGateway(undefined, { heartbeatInterval: 0 })
    await gateway(fake.url).connect()
    await wait(300)
    const beats = fake.server.sockets[0]!.received.filter((f: any) => f.op === 1).length
    assert.ok(beats <= 1, `${beats} heartbeats in 300 ms`)
  })
})

describe('retry_after from the server', () => {
  test('a wait beyond the cap fails the request at once with the 429 and holds back nothing', async () => {
    let n = 0
    server = await startHttpServer((req, res) => {
      if (n++ === 0) return sendJson(res, 429, { message: 'You are being rate limited.', retry_after: 3_000_000, global: true })
      sendJson(res, 200, { id: req.url })
    })
    const rest = new RestClient({ baseUrl: server.url, token: 'mnb_secret' })
    // Each request has its own deadline: without the cap the client would wait
    // (or spin) for weeks, and the test must fail fast instead of hanging.
    const start = Date.now()
    const err = await rest.request('GET', '/users/@me', { signal: AbortSignal.timeout(3000) }).catch((e: unknown) => e)
    assert.ok(err instanceof RestError, `a 429 beyond the cap fails at once, got ${String(err)}`)
    assert.equal(err.status, 429)
    assert.equal(err.retryAfter, 3_000_000)
    const other = await rest.request('GET', '/channels', { signal: AbortSignal.timeout(3000) })
    assert.ok(other)
    assert.ok(Date.now() - start < 2000)
  })
})

describe('manifests the server would refuse', () => {
  const base: Manifest = { manifest_version: 1, name: 'helper' }

  test('unknown intents and permissions are refused before a link is built', () => {
    assert.throws(() => encodeManifest({ ...base, intents: ['MESSAGE_CONTENT' as never] }), ValidationError)
    assert.throws(() => encodeManifest({ ...base, permissions: ['administrator' as never] }), ValidationError)
  })

  test('an avatar above 256 KB is refused', () => {
    const big = `data:image/png;base64,${Buffer.alloc(300 * 1024).toString('base64')}`
    assert.throws(() => encodeManifest({ ...base, avatar: big }), /256 KB/)
  })
})

describe('uploads', () => {
  test('the text fields come before the file, so the server checks the target first', async () => {
    server = await startHttpServer((_req, res) => sendJson(res, 201, { id: 'm1' }))
    const rest = new RestClient({ baseUrl: server.url, token: 'mnb_secret' })
    await rest.uploadFile('c', { file: new Uint8Array([1, 2, 3]), name: 'a.bin', content: 'hi', parent_id: 'p' })
    const body = server.requests[0]!.body.toString('latin1')
    assert.ok(body.indexOf('name="content"') < body.indexOf('name="file"'))
    assert.ok(body.indexOf('name="parent_id"') < body.indexOf('name="file"'))
  })
})
