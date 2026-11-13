// Contract tests against a real server: the SDK and the server must agree on
// every shape used here. They run only when MNEMA_CONTRACT_URL is set; see
// the README (Contract tests) for the environment.
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import {
  actionRow,
  button,
  Client,
  Intents,
  modal,
  RestClient,
  RestError,
  textInput,
  type ClientEvents,
  type Interaction,
  type RateLimitInfo,
} from '../../src/index.ts'
import { contractEnv, Member, withTimeout, type ContractEnv } from './member.ts'

const envOrSkip = contractEnv()
const skip = typeof envOrSkip === 'string' ? envOrSkip : false
const env = envOrSkip as ContractEnv
const intents = Intents.Guilds | Intents.GuildMessages | Intents.GuildMessageReactions
const run = Math.random().toString(36).slice(2, 8)

function next<K extends keyof ClientEvents>(c: Client, event: K, pred: (...a: ClientEvents[K]) => boolean = () => true): Promise<ClientEvents[K]> {
  return withTimeout(
    new Promise((resolve) => {
      const listener = (...args: ClientEvents[K]) => {
        if (!pred(...args)) return
        c.off(event, listener)
        resolve(args)
      }
      c.on(event, listener)
    }),
    10_000,
    `the ${String(event)} event`,
  )
}

const nextInteraction = (c: Client, pred: (i: Interaction) => boolean) => next(c, 'interactionCreate', pred).then(([i]) => i)

describe('contract with a running server', { skip }, () => {
  let client: Client
  let member: Member
  let botUserId = ''

  before(() => {
    client = new Client({ baseUrl: env.url, token: env.token, intents })
    member = new Member(env)
  })

  after(async () => {
    member?.close()
    await client?.close()
  })

  let botHandle = ''
  test('GET /users/@me answers the bot with its allowed intents', async () => {
    const me = await client.rest.getMe()
    assert.equal(me.bot, true)
    assert.match(me.id, /^[0-9a-f-]{36}$/)
    assert.equal(me.allowed_intents & intents, intents, `allowed_intents ${me.allowed_intents} lacks ${intents}`)
    assert.match(me.handle ?? "", /^bot-/, 'the handle is the name members mention')
    botUserId = me.id
    botHandle = me.handle ?? ""
  })

  test('connect identifies and READY carries the session and the granted intents', async () => {
    const ready = await withTimeout(client.connect(), 15_000, 'READY')
    assert.equal(ready.v, 1)
    assert.equal(ready.user.id, botUserId)
    assert.equal(ready.user.bot, true)
    assert.equal(ready.intents, intents)
    assert.ok(ready.session_id.length > 0)
    assert.match(ready.resume_gateway_url, /^wss?:\/\//)
    assert.ok(ready.heartbeat_interval > 0)
    assert.equal(ready.user.handle, botHandle)
    assert.equal(client.user?.id, botUserId)
  })

  test('a posted message comes back as MESSAGE_CREATE', async () => {
    const content = `contract ${run}: hello`
    const seen = next(client, 'messageCreate', (m) => m.content === content)
    const posted = await client.rest.createMessage(env.channel, { content, allowed_mentions: { parse: [] } })
    assert.equal(posted.channel_id, env.channel)
    assert.equal(posted.user_id, botUserId)
    const [event] = await seen
    assert.equal(event.id, posted.id)
    assert.equal((await client.rest.getMessage(env.channel, posted.id)).content, content)
  })

  let commandId = ''
  test('a command is registered and listed', async () => {
    const cmd = await client.commands.create({
      name: 'contract-echo',
      description: 'Echoes the text (contract test)',
      options: [{ name: 'text', description: 'What to echo', type: 3, required: true }],
    })
    assert.equal(cmd.name, 'contract-echo')
    assert.deepEqual(cmd.options.map((o) => [o.name, o.type, o.required]), [['text', 3, true]])
    commandId = cmd.id
    assert.ok((await client.commands.list()).some((c) => c.id === commandId))
  })

  test('the member runs the command and the SDK replies', async () => {
    const got = nextInteraction(client, (i) => i.isCommand() && i.commandName === 'contract-echo')
    const invoked = await member.call('POST', `/api/channels/${env.channel}/interactions`, { command_id: commandId, options: { text: run } }, 202)
    const i = await got
    assert.equal(i.id, invoked.interaction_id)
    assert.equal(i.channelId, env.channel)
    assert.equal(i.options.get('text'), run)
    assert.equal(i.commandId, commandId)
    assert.ok(i.user.id)
    const reply = next(client, 'messageCreate', (m) => m.content === `echo: ${run}`)
    await i.reply({ content: `echo: ${run}` })
    const [m] = await reply
    assert.equal(m.user_id, botUserId)
    commandRun = i
  })

  let commandRun: Interaction | undefined
  test('the answers take allowed_mentions like a post, and @original takes no flags', async () => {
    const i = commandRun!
    const name = `@${i.user.username}`
    const quiet = await i.followUp({ content: `quiet ${name}`, allowed_mentions: { parse: [] } })
    assert.ok(quiet && !quiet.mentions.includes(i.user.id), 'parse [] notifies nobody')
    const loud = await i.followUp({ content: `loud ${name}` })
    assert.ok(loud && loud.mentions.includes(i.user.id), 'without allowed_mentions the member named is notified')
    assert.equal(await i.followUp({ content: 'psst', ephemeral: true }), undefined)
    const edited = await i.editReply({ content: `edited ${name}`, allowed_mentions: { parse: [] } })
    assert.ok(edited && !edited.mentions.includes(i.user.id))
    await assert.rejects(i.editReply({ content: 'x', ephemeral: true } as never), TypeError)
    await assert.rejects(
      client.rest.editOriginalResponse(i.id, i.token, { content: 'x', flags: 64 } as never),
      (e: unknown) => e instanceof RestError && e.status === 400 && /flags cannot be set on @original/.test(e.message),
    )
  })

  test('a button click reaches the SDK, which updates the message the member sees', async () => {
    const posted = await client.rest.createMessage(env.channel, {
      content: `contract ${run}: click me`,
      components: [actionRow(button({ customId: `click:${run}`, label: 'Click' }))],
    })
    assert.equal(posted.components?.[0]?.components[0]?.custom_id, `click:${run}`)
    const got = nextInteraction(client, (i) => i.isButton() && i.customId === `click:${run}`)
    await member.call('POST', `/api/channels/${env.channel}/messages/${posted.id}/components`, { custom_id: `click:${run}` }, 202)
    const i = await got
    assert.equal(i.message?.id, posted.id)
    assert.equal(i.componentType, 2)
    await i.update({ content: `contract ${run}: clicked`, components: [] })
    const seen = await member.message(posted.id)
    assert.equal(seen.content, `contract ${run}: clicked`)
    assert.ok(!seen.components || seen.components.length === 0, 'the components were removed')
  })

  test('a modal opened on a click is submitted by the member and answered', async () => {
    await member.listen()
    const posted = await client.rest.createMessage(env.channel, {
      content: `contract ${run}: feedback`,
      components: [actionRow(button({ customId: `form:${run}`, label: 'Feedback' }))],
    })
    const click = nextInteraction(client, (i) => i.isButton() && i.customId === `form:${run}`)
    await member.call('POST', `/api/channels/${env.channel}/messages/${posted.id}/components`, { custom_id: `form:${run}` }, 202)
    await (await click).showModal(modal({ customId: `fb:${run}`, title: 'Feedback', components: [textInput({ customId: 'subject', label: 'Subject' })] }))
    const opened = await member.event('interaction_modal', (p) => p.title === 'Feedback')
    assert.equal(opened.channel_id, env.channel)
    assert.equal(opened.components[0].components[0].custom_id, 'subject')
    const submitted = nextInteraction(client, (i) => i.isModalSubmit() && i.customId === `fb:${run}`)
    await member.call('POST', `/api/channels/${env.channel}/modals/${opened.id}`, { values: { subject: 'Login' } }, 202)
    const s = await submitted
    assert.equal(s.fields.get('subject'), 'Login')
    assert.equal(s.message?.id, posted.id)
    await s.reply({ content: `thanks ${run}`, ephemeral: true })
  })

  test('429 answers carry retry_after and the SDK waits and retries', async () => {
    const limits: RateLimitInfo[] = []
    const raw = new RestClient({ baseUrl: env.url, token: env.token, localRateLimits: false, maxRetries: 5, onRateLimit: (i) => limits.push(i) })
    const posts = Array.from({ length: 7 }, (_, n) => raw.createMessage(env.channel, `contract ${run}: burst ${n}`))
    const messages = await Promise.all(posts)
    assert.equal(messages.length, 7)
    assert.ok(limits.length > 0, 'the server answered 429 to the burst')
    const first = limits[0]!
    assert.equal(first.global, false)
    assert.ok(first.retryAfter > 0 && first.retryAfter <= 5, `retry_after ${first.retryAfter}`)
    assert.equal(first.bucket, `post-message:${env.channel}`)
    assert.equal(first.scope, 'user')
    for (const m of messages) await client.rest.deleteMessage(env.channel, m.id)
  })

  test('the command is deleted and close() ends the connection', async () => {
    await client.commands.delete(commandId)
    assert.ok(!(await client.commands.list()).some((c) => c.id === commandId))
    const closed = next(client, 'close')
    await client.close()
    const [info] = await closed
    assert.equal(info.reconnect, false)
  })
})
