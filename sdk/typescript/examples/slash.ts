// Slash command bot: /ping answers at once, /roll defers and then edits its
// answer, and either can answer only to the member who ran it.
// Run: MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... node examples/slash.ts
import { Client, OptionType } from 'mnema-talk-sdk'

const client = new Client({
  baseUrl: process.env.MNEMA_URL ?? 'https://talk.example.org',
  token: process.env.MNEMA_TOKEN ?? '',
  intents: 0, // interactions need no intent
})

client.on('ready', async () => {
  const commands = await client.commands.set([
    {
      name: 'ping',
      description: 'Replies with pong',
      options: [{ name: 'private', description: 'Only you see the answer', type: OptionType.Boolean }],
    },
    {
      name: 'roll',
      description: 'Rolls a die',
      options: [
        { name: 'sides', description: 'How many sides', type: OptionType.Integer, required: true },
        { name: 'label', description: 'What the roll is for', type: OptionType.String, choices: ['attack', 'defence', 'luck'] },
      ],
    },
  ])
  console.log(`registered ${commands.map((c) => '/' + c.name).join(', ')}`)
})

client.on('interactionCreate', async (i) => {
  if (!i.isCommand()) return
  if (i.commandName === 'ping') {
    await i.reply({ content: 'pong', ephemeral: i.options.get('private') === true })
    return
  }
  if (i.commandName === 'roll') {
    await i.deferReply()
    const sides = Number(i.options.get('sides'))
    if (!Number.isInteger(sides) || sides < 2) {
      await i.editReply('A die needs at least 2 sides.')
      return
    }
    const label = i.options.get('label')
    const result = 1 + Math.floor(Math.random() * sides)
    await i.editReply(`${i.user.display_name} rolled **${result}** (d${sides}${label ? `, ${label}` : ''})`)
  }
})

client.on('error', (err) => console.error(err))
process.on('SIGINT', () => void client.close().then(() => process.exit(0)))

await client.connect()
