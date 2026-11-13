import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { closeAction, FrameTooLargeError, gatewayUrlFor } from '../src/index.ts'
import { Outbox } from '../src/gateway/outbox.ts'
import { backoffDelay } from '../src/gateway/policy.ts'
import { bucketKey, WindowLimiter } from '../src/rest/limits.ts'

describe('rest buckets', () => {
  test('channels keep their own buckets, other ids share one', () => {
    const ch = '0b5c2f0e-8d3a-4c1b-9e2f-1a2b3c4d5e6f'
    const msg = '1c6d3f1f-9e4b-4d2c-8f3a-2b3c4d5e6f70'
    assert.equal(bucketKey('POST', `/channels/${ch}/messages`), `post-message:${ch}`)
    assert.equal(bucketKey('POST', `/channels/${ch}/upload`), `post-message:${ch}`)
    assert.equal(bucketKey('PATCH', `/channels/${ch}/messages/${msg}`), `PATCH /channels/${ch}/messages/:id`)
    assert.equal(bucketKey('PUT', `/channels/${ch}/messages/${msg}/reactions/%F0%9F%91%8D/@me`), `PUT /channels/${ch}/messages/:id/reactions`)
    assert.equal(bucketKey('POST', '/interactions/abc/tok/callback'), 'POST /interactions/:id/:token/callback')
  })

  test('a window allows its limit and then says how long to wait', () => {
    const w = new WindowLimiter(5, 5000)
    for (let t = 0; t < 5; t++) {
      assert.equal(w.delay(t * 10), 0)
      w.take(t * 10)
    }
    assert.equal(w.delay(100), 4900)
    assert.equal(w.delay(5000), 0)
  })
})

describe('gateway frames', () => {
  test('a frame over 4096 bytes is refused', () => {
    const o = new Outbox()
    o.attach(() => {})
    assert.throws(() => o.send({ op: 3, d: 'x'.repeat(5000) }), FrameTooLargeError)
    o.reset()
  })

  test('at most 115 ordinary frames a minute, leaving room for heartbeats', () => {
    const sent: string[] = []
    const o = new Outbox()
    o.attach((t) => sent.push(t))
    for (let i = 0; i < 130; i++) o.send({ op: 3, d: i })
    assert.equal(sent.length, 115)
    assert.equal(o.pending, 15)
    for (let i = 0; i < 5; i++) o.send({ op: 1, d: i }, true)
    assert.equal(sent.length, 120)
    o.reset()
    assert.equal(o.pending, 0)
  })
})

describe('reconnect policy', () => {
  test('close codes map to what happens next', () => {
    assert.equal(closeAction(4004), 'fatal')
    assert.equal(closeAction(4013), 'fatal')
    assert.equal(closeAction(4014), 'fatal')
    assert.equal(closeAction(4008), 'rateLimited')
    assert.equal(closeAction(4000), 'identify')
    assert.equal(closeAction(4005), 'identify')
    assert.equal(closeAction(4009), 'resume')
    assert.equal(closeAction(1006), 'resume')
    assert.equal(closeAction(1013), 'resume')
  })

  test('backoff doubles from 1 s up to 60 s with jitter', () => {
    const o = { minDelayMs: 1000, maxDelayMs: 60_000, rateLimitedDelayMs: 30_000 }
    assert.deepEqual([0, 1, 2, 3].map((n) => backoffDelay(n, o, () => 1)), [1000, 2000, 4000, 8000])
    assert.equal(backoffDelay(10, o, () => 1), 60_000)
    assert.equal(backoffDelay(10, o, () => 0), 30_000)
  })

  test('the gateway address is derived from the server address', () => {
    assert.equal(gatewayUrlFor('https://talk.example.org/'), 'wss://talk.example.org/api/bot/v1/gateway')
    assert.equal(gatewayUrlFor('http://127.0.0.1:8080'), 'ws://127.0.0.1:8080/api/bot/v1/gateway')
  })
})
