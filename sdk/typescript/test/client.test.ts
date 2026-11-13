import assert from 'node:assert/strict'
import type { IncomingMessage, ServerResponse } from 'node:http'
import { afterEach, describe, test } from 'node:test'
import { Client, Intents, Interaction, modal, RestError, textInput, type ClientEvents } from '../src/index.ts'
import { startFakeGateway, type FakeGateway } from './helpers/fakeGateway.ts'
import type { TestSocket } from './helpers/wsServer.ts'

interface Call {
  method: string
  url: string
  body: any
}

let fake: FakeGateway | undefined
let client: Client | undefined
let calls: Call[] = []

afterEach(async () => {
  await client?.close()
  await fake?.close()
  client = undefined
  fake = undefined
  calls = []
})

function rest(req: IncomingMessage, res: ServerResponse): void {
  let body = ''
  req.on('data', (c) => (body += c))
  req.on('end', () => {
    calls.push({ method: req.method ?? '', url: req.url ?? '', body: body ? JSON.parse(body) : undefined })
    res.setHeader('Content-Type', 'application/json')
    if (req.url === '/api/bot/v1/users/@me') return void res.end(JSON.stringify({ id: 'user-1', bot: true }))
    if (req.url === '/api/bot/v1/gateway') return void res.end(JSON.stringify({ url: fake!.url, heartbeat_interval: 41250, max_connections: 3 }))
    res.writeHead(204).end()
  })
}

async function connected(): Promise<{ c: Client; sock: TestSocket }> {
  fake = await startFakeGateway(rest)
  client = new Client({ baseUrl: fake.httpUrl, token: 'mnb_t', intents: Intents.GuildMessages | Intents.GuildMessageReactions })
  await client.connect()
  return { c: client, sock: await fake.server.nextSocket() }
}

function next<K extends keyof ClientEvents>(c: Client, event: K): Promise<ClientEvents[K]> {
  return new Promise((resolve) => c.once(event, (...args) => resolve(args)))
}

const interaction = (type: 2 | 3 | 5, data: object, extra: object = {}) => ({
  id: 'i1',
  token: 'tok',
  type,
  channel_id: 'ch',
  application_id: 'bot-1',
  data,
  member: { user: { id: 'u1', username: 'max', display_name: 'Max' } },
  ...extra,
})

describe('connect', () => {
  test('checks the token, finds the gateway through GET /gateway and keeps the user of READY', async () => {
    const { c } = await connected()
    assert.deepEqual(calls.map((x) => x.url), ['/api/bot/v1/users/@me', '/api/bot/v1/gateway'])
    assert.equal(c.user?.id, 'user-1')
    assert.equal(c.user?.bot_id, 'bot-1')
  })

  test('a refused token fails connect() before the gateway is opened', async () => {
    fake = await startFakeGateway((_req, res) => {
      res.writeHead(401, { 'Content-Type': 'application/json' }).end(JSON.stringify({ message: '401: Unauthorized', code: 0 }))
    })
    client = new Client({ baseUrl: fake.httpUrl, token: 'mnb_wrong', intents: Intents.GuildMessages })
    await assert.rejects(client.connect(), (e: RestError) => e instanceof RestError && e.status === 401)
    assert.equal(fake.server.sockets.length, 0)
  })
})

describe('dispatches become typed events', () => {
  test('messages, deletes, reactions, channels and members', async () => {
    const { c, sock } = await connected()
    const created = next(c, 'messageCreate')
    fake!.dispatch(sock, 'MESSAGE_CREATE', { id: 'm1', channel_id: 'ch', content: '!echo hi' })
    assert.equal((await created)[0].content, '!echo hi')
    const updated = next(c, 'messageUpdate')
    fake!.dispatch(sock, 'MESSAGE_UPDATE', { id: 'm1', content: 'edited' })
    assert.equal((await updated)[0].content, 'edited')
    const deleted = next(c, 'messageDelete')
    fake!.dispatch(sock, 'MESSAGE_DELETE', { id: 'm1', channel_id: 'ch', parent_id: null })
    assert.equal((await deleted)[0].channel_id, 'ch')
    const reactions = next(c, 'reactionsUpdate')
    fake!.dispatch(sock, 'MESSAGE_REACTIONS_UPDATE', { message_id: 'm1', channel_id: 'ch', reactions: [{ emoji: '👍', count: 1, users: ['u1'] }] })
    assert.equal((await reactions)[0].reactions[0]?.count, 1)
    const channels = next(c, 'channelsUpdate')
    fake!.dispatch(sock, 'CHANNELS_UPDATE', {})
    await channels
    const member = next(c, 'memberAdd')
    fake!.dispatch(sock, 'GUILD_MEMBER_ADD', { id: 'u2', username: 'eva', display_name: 'Eva' })
    assert.equal((await member)[0].username, 'eva')
  })

  test('a failing listener is reported on the error event', async () => {
    const { c, sock } = await connected()
    c.on('messageCreate', async () => {
      throw new Error('listener broke')
    })
    const err = next(c, 'error')
    fake!.dispatch(sock, 'MESSAGE_CREATE', { id: 'm1' })
    assert.equal((await err)[0].message, 'listener broke')
  })
})

async function interactionOf(payload: object): Promise<Interaction> {
  const { c, sock } = await connected()
  const got = next(c, 'interactionCreate')
  fake!.dispatch(sock, 'INTERACTION_CREATE', payload)
  return (await got)[0]
}

describe('interactions', () => {
  test('a slash command exposes its options and replies ephemerally', async () => {
    const i = await interactionOf(interaction(2, { id: 'cmd', name: 'roll', options: [{ name: 'sides', type: 4, value: 20 }, { name: 'loud', type: 5, value: true }] }))
    assert.ok(i.isCommand() && !i.isComponent() && !i.isModalSubmit())
    assert.equal(i.commandName, 'roll')
    assert.equal(i.options.get('sides'), 20)
    assert.equal(i.options.get('loud'), true)
    assert.equal(i.user.username, 'max')
    await i.reply({ content: 'You rolled 7', ephemeral: true })
    const cb = calls.at(-1)!
    assert.equal(cb.url, '/api/bot/v1/interactions/i1/tok/callback')
    assert.deepEqual(cb.body, { type: 4, data: { content: 'You rolled 7', flags: 64 } })
    await assert.rejects(i.reply('again'), /already answered/)
  })

  test('deferReply, editReply, followUp and deleteReply use their routes', async () => {
    const i = await interactionOf(interaction(2, { id: 'cmd', name: 'ping' }))
    await i.deferReply({ ephemeral: true })
    await i.editReply('pong')
    await i.followUp({ content: 'more' })
    await i.deleteReply()
    assert.deepEqual(calls.slice(-4).map((x) => [x.method, x.url.replace('/api/bot/v1/interactions/i1/tok', ''), x.body]), [
      ['POST', '/callback', { type: 5, data: { flags: 64 } }],
      ['PATCH', '/messages/@original', { content: 'pong' }],
      ['POST', '/followups', { content: 'more' }],
      ['DELETE', '/messages/@original', undefined],
    ])
    assert.ok(i.deferred)
  })

  test('a button click updates its message', async () => {
    const i = await interactionOf(interaction(3, { custom_id: 'again', component_type: 2 }, { message: { id: 'm1', content: 'pong' } }))
    assert.ok(i.isButton() && !i.isSelect())
    assert.equal(i.customId, 'again')
    assert.equal(i.message?.id, 'm1')
    await i.update({ content: 'pong again', components: [] })
    assert.deepEqual(calls.at(-1)!.body, { type: 7, data: { content: 'pong again', components: [] } })
  })

  test('a select carries its values and resolved data; deferUpdate acknowledges', async () => {
    const resolved = { users: { u2: { id: 'u2', username: 'eva', display_name: 'Eva' } } }
    const i = await interactionOf(interaction(3, { custom_id: 'who', component_type: 5, values: ['u2'], resolved }, { message: { id: 'm1' } }))
    assert.ok(i.isSelect())
    assert.deepEqual(i.values, ['u2'])
    assert.equal(i.resolved?.users?.u2?.username, 'eva')
    await i.deferUpdate()
    assert.deepEqual(calls.at(-1)!.body, { type: 6 })
  })

  test('showModal opens a modal; its submit exposes the fields', async () => {
    const click = await interactionOf(interaction(3, { custom_id: 'feedback', component_type: 2 }, { message: { id: 'm1' } }))
    await click.showModal(modal({ customId: 'fb', title: 'Feedback', components: [textInput({ customId: 'subject', label: 'Subject' })] }))
    assert.deepEqual(calls.at(-1)!.body, {
      type: 9,
      data: { custom_id: 'fb', title: 'Feedback', components: [{ type: 1, components: [{ type: 4, custom_id: 'subject', label: 'Subject', style: 1 }] }] },
    })
    const submitPayload = interaction(5, {
      custom_id: 'fb',
      components: [
        { type: 1, components: [{ type: 4, custom_id: 'subject', value: 'Login' }] },
        { type: 1, components: [{ type: 4, custom_id: 'body', value: '' }] },
      ],
    })
    const submit = new Interaction(submitPayload as never, client!.rest)
    assert.ok(submit.isModalSubmit())
    assert.equal(submit.customId, 'fb')
    assert.deepEqual([...submit.fields], [['subject', 'Login'], ['body', '']])
    await assert.rejects(submit.showModal(modal({ customId: 'x', title: 'X', components: [textInput({ customId: 'a', label: 'A' })] })), /cannot open another modal/)
    await assert.rejects(submit.update('x'), /only for a component interaction/)
  })
})

describe('commands', () => {
  test('set, create, list and delete go to /commands', async () => {
    const { c } = await connected()
    await c.commands.set([{ name: 'ping', description: 'Replies with pong' }])
    await c.commands.create({ name: 'roll', description: 'Rolls a die', options: [{ name: 'sides', description: 'Sides', type: 4 }] })
    await c.commands.list()
    await c.commands.delete('cmd-1')
    assert.deepEqual(calls.slice(-4).map((x) => `${x.method} ${x.url}`), [
      'PUT /api/bot/v1/commands',
      'POST /api/bot/v1/commands',
      'GET /api/bot/v1/commands',
      'DELETE /api/bot/v1/commands/cmd-1',
    ])
  })
})
