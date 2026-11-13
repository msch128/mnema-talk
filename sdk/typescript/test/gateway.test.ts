import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'
import { Gateway, GatewayError, Intents, type GatewayOptions } from '../src/index.ts'
import { startFakeGateway, type FakeGateway } from './helpers/fakeGateway.ts'

let fake: FakeGateway | undefined
let gw: Gateway | undefined

afterEach(async () => {
  await gw?.close()
  await fake?.close()
  gw = undefined
  fake = undefined
})

function gateway(url: string, extra: Partial<GatewayOptions> = {}): Gateway {
  gw = new Gateway({
    token: 'mnb_test',
    intents: [Intents.GuildMessages, Intents.Guilds],
    url,
    reconnect: { minDelayMs: 10, maxDelayMs: 40, rateLimitedDelayMs: 300 },
    random: () => 0,
    ...extra,
  })
  return gw
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))

describe('identify', () => {
  test('authenticates the upgrade and identifies with the intents and presence', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url, { presence: { status: 'idle', activity: { name: 'chess', type: 0 } } })
    const ready = await g.connect()
    const sock = fake.server.sockets[0]!
    assert.equal(sock.request.headers.authorization, 'Bot mnb_test')
    const identify = await sock.waitForOp(2)
    assert.equal(identify.d.intents, 513)
    assert.equal(identify.d.token, 'mnb_test')
    assert.equal(typeof identify.d.properties.os, 'string')
    assert.deepEqual(identify.d.presence, { since: null, afk: false, status: 'idle', activities: [{ name: 'chess', type: 0 }] })
    assert.equal(ready.session_id, 'session-1')
    assert.equal(ready.user.id, 'user-1')
  })

  test('resolves the address through a function, as the client does with GET /gateway', async () => {
    fake = await startFakeGateway()
    const url = fake.url
    const ready = await gateway('', { url: async () => url }).connect()
    assert.equal(ready.v, 1)
  })
})

describe('heartbeats', () => {
  test('beats at the interval with the last sequence number', async () => {
    fake = await startFakeGateway(undefined, { heartbeatInterval: 30 })
    const g = gateway(fake.url)
    await g.connect()
    const sock = fake.server.sockets[0]!
    fake.dispatch(sock, 'MESSAGE_CREATE', { id: 'm1' })
    await sock.waitFor((f) => f.op === 1 && f.d === 2)
    const beats = sock.received.filter((f: any) => f.op === 1)
    assert.ok(beats.length >= 1)
  })

  test('the first beat comes after a random part of the interval', async () => {
    fake = await startFakeGateway(undefined, { heartbeatInterval: 400 })
    const g = gateway(fake.url, { random: () => 0.5 })
    await g.connect()
    const sock = fake.server.sockets[0]!
    await wait(100)
    assert.equal(sock.received.filter((f: any) => f.op === 1).length, 0)
    await sock.waitForOp(1)
  })

  test('a missing ACK drops the connection and resumes on a new one', async () => {
    fake = await startFakeGateway(undefined, { heartbeatInterval: 30, ack: false })
    const g = gateway(fake.url)
    await g.connect()
    const closes: number[] = []
    g.on('close', (c) => closes.push(c.code))
    const second = await fake.server.nextSocket().then(() => fake!.server.nextSocket())
    const resume = await second.waitForOp(6)
    assert.equal(resume.d.session_id, 'session-1')
    assert.equal(resume.d.seq, 1)
    assert.deepEqual(closes, [4900])
  })
})

describe('resume', () => {
  test('after a drop the next connection resumes with session_id and seq', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url)
    await g.connect()
    const first = await fake.server.nextSocket()
    fake.dispatch(first, 'MESSAGE_CREATE', { id: 'm1' })
    fake.dispatch(first, 'MESSAGE_CREATE', { id: 'm2' })
    await wait(20)
    const resumed = new Promise<void>((r) => g.once('resumed', r))
    first.destroy()
    const second = await fake.server.nextSocket()
    const resume = await second.waitForOp(6)
    assert.deepEqual(resume.d, { token: 'mnb_test', session_id: 'session-1', seq: 3 })
    await resumed
    assert.equal(second.received.filter((f: any) => f.op === 2).length, 0)
  })

  test('INVALID_SESSION makes the bot identify on the same connection', async () => {
    fake = await startFakeGateway(undefined, { resumable: false })
    const g = gateway(fake.url)
    await g.connect()
    const first = await fake.server.nextSocket()
    first.destroy()
    const second = await fake.server.nextSocket()
    await second.waitForOp(6)
    const identify = await second.waitForOp(2)
    assert.equal(identify.d.intents, 513)
    await new Promise((r) => g.once('ready', r))
    assert.equal(g.session?.id, 'session-2')
  })

  test('a drop older than the resume window identifies instead', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url, { resumeWindowMs: 0 })
    await g.connect()
    ;(await fake.server.nextSocket()).destroy()
    const second = await fake.server.nextSocket()
    await second.waitForOp(2)
    assert.equal(second.received.filter((f: any) => f.op === 6).length, 0)
  })

  test('4000 (session taken over) starts a new session', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url)
    await g.connect()
    ;(await fake.server.nextSocket()).close(4000, 'Session resumed elsewhere')
    const second = await fake.server.nextSocket()
    await second.waitForOp(2)
    assert.equal(second.received.filter((f: any) => f.op === 6).length, 0)
  })
})

describe('fatal close codes', () => {
  for (const code of [4004, 4013, 4014]) {
    test(`${code} stops, rejects connect() and reports the error`, async () => {
      fake = await startFakeGateway(undefined, { onIdentify: (s) => s.close(code, 'refused') })
      const g = gateway(fake.url)
      const errors: Error[] = []
      g.on('error', (e) => errors.push(e))
      await assert.rejects(g.connect(), (e: unknown) => e instanceof GatewayError && e.code === code)
      await wait(80)
      assert.equal(fake.server.sockets.length, 1)
      assert.equal(errors.length, 1)
    })
  }

  test('a fatal close after READY is reported on the error event', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url)
    await g.connect()
    const err = new Promise<Error>((r) => g.once('error', r))
    ;(await fake.server.nextSocket()).close(4004, 'Authentication failed')
    assert.equal(((await err) as GatewayError).code, 4004)
  })
})

describe('backoff', () => {
  test('4008 waits at least the rate-limited delay before reconnecting', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url)
    await g.connect()
    const start = Date.now()
    ;(await fake.server.nextSocket()).close(4008, 'Rate limited')
    await fake.server.nextSocket()
    assert.ok(Date.now() - start >= 290, `reconnected after ${Date.now() - start} ms`)
  })

  test('failed connections back off exponentially', async () => {
    fake = await startFakeGateway(undefined, { onIdentify: (s) => s.destroy() })
    const g = gateway(fake.url, { reconnect: { minDelayMs: 20, maxDelayMs: 80, rateLimitedDelayMs: 0 }, random: () => 1 })
    void g.connect().catch(() => {})
    const times: number[] = []
    for (let i = 0; i < 4; i++) {
      await fake.server.nextSocket()
      times.push(Date.now())
    }
    const gaps = times.slice(1).map((t, i) => t - times[i]!)
    assert.ok(gaps[0]! >= 18 && gaps[1]! >= 38 && gaps[2]! >= 78, `gaps ${gaps.join(', ')}`)
  })
})

describe('close', () => {
  test('stops for good and leaves no timers behind', async () => {
    const before = process.getActiveResourcesInfo().filter((r) => r === 'Timeout').length
    fake = await startFakeGateway(undefined, { heartbeatInterval: 20 })
    const g = gateway(fake.url)
    await g.connect()
    await wait(50)
    await g.close()
    await wait(20)
    const after = process.getActiveResourcesInfo().filter((r) => r === 'Timeout').length
    assert.ok(after <= before, `timers before ${before}, after ${after}`)
    assert.equal(fake.server.sockets.length, 1)
  })

  test('close() while connecting rejects connect()', async () => {
    fake = await startFakeGateway(undefined, { onIdentify: () => {} })
    const g = gateway(fake.url)
    const p = g.connect()
    await fake.server.nextSocket()
    await g.close()
    await assert.rejects(p, /closed/)
  })
})

describe('presence', () => {
  test('setPresence sends op 3 once identified and refuses a bad status', async () => {
    fake = await startFakeGateway()
    const g = gateway(fake.url)
    await g.connect()
    g.setPresence({ status: 'dnd', activity: { name: 'tests', type: 3 } })
    const sock = await fake.server.nextSocket()
    const p = await sock.waitForOp(3)
    assert.deepEqual(p.d, { since: null, afk: false, status: 'dnd', activities: [{ name: 'tests', type: 3 }] })
    assert.throws(() => g.setPresence({ status: 'away' as never }), /presence status/)
  })
})
