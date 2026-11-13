// What each answer of an interaction sends: exactly what its route accepts.
import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'
import { Interaction, RestClient, type InteractionPayload } from '../src/index.ts'
import { sendJson, startHttpServer, type TestHttpServer } from './helpers/httpServer.ts'

let server: TestHttpServer | undefined
afterEach(async () => {
  await server?.close()
  server = undefined
})

const payload: InteractionPayload = {
  id: 'i1', token: 't1', type: 2, channel_id: 'c1', application_id: 'b1',
  data: { id: 'cmd', name: 'ping' },
  member: { user: { id: 'u1', username: 'max', display_name: 'Max' } },
} as InteractionPayload

async function interaction(): Promise<Interaction> {
  server = await startHttpServer((req, res) => (req.method === 'POST' && req.url.endsWith('/callback') ? res.writeHead(204).end() : sendJson(res, 200, { id: 'm1', mentions: [] })))
  return new Interaction(payload, new RestClient({ baseUrl: server.url, token: 'mnb_t' }))
}

describe('interaction answers', () => {
  test('reply and followUp send allowed_mentions and turn ephemeral into flags', async () => {
    const i = await interaction()
    await i.reply({ content: 'hi', ephemeral: true, allowed_mentions: { parse: [] } })
    await i.followUp({ content: 'more', allowed_mentions: { parse: ['users'] } })
    const [cb, fu] = server!.requests.map((r) => r.json())
    assert.deepEqual(cb, { type: 4, data: { content: 'hi', allowed_mentions: { parse: [] }, flags: 64 } })
    assert.deepEqual(fu, { content: 'more', allowed_mentions: { parse: ['users'] } })
  })

  test('editReply sends allowed_mentions and refuses ephemeral or flags before any request', async () => {
    const i = await interaction()
    await i.editReply({ content: 'edited', allowed_mentions: { parse: [] } })
    assert.deepEqual(server!.requests[0]!.json(), { content: 'edited', allowed_mentions: { parse: [] } })
    await assert.rejects(i.editReply({ content: 'x', ephemeral: true } as never), TypeError)
    await assert.rejects(i.editReply({ content: 'x', flags: 64 } as never), TypeError)
    assert.equal(server!.requests.length, 1)
  })

  test('every interaction names its member; one without is refused', () => {
    const rest = new RestClient({ baseUrl: 'http://127.0.0.1:9', token: 'mnb_t' })
    assert.equal(new Interaction(payload, rest).user.username, 'max')
    assert.throws(() => new Interaction({ ...payload, member: undefined } as never, rest), TypeError)
  })
})
