// Echo bot: answers "!echo <text>" with the text, as a reply.
// Run: MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... node examples/echo.ts
// (after `npm run build`; the bot needs View channels and Send messages).
import { Client, Intents } from 'mnema-talk-sdk'

const client = new Client({
  baseUrl: process.env.MNEMA_URL ?? 'https://talk.example.org',
  token: process.env.MNEMA_TOKEN ?? '',
  intents: Intents.GuildMessages,
  presence: { status: 'online', activity: { name: '!echo', type: 2 } },
})

client.on('ready', (ready) => console.log(`connected as ${ready.user.username}`))

client.on('messageCreate', async (message) => {
  // The bot's own messages arrive too: ignore them.
  if (message.user_id === client.user?.id || !message.content.startsWith('!echo ')) return
  await client.rest.createMessage(message.channel_id, {
    content: message.content.slice('!echo '.length),
    message_reference: { message_id: message.id },
    allowed_mentions: { parse: [] },
  })
})

client.on('error', (err) => console.error(err))
process.on('SIGINT', () => void client.close().then(() => process.exit(0)))

await client.connect()
