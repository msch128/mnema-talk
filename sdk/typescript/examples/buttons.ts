// Buttons, a select and a modal: "!deploy" posts a message with buttons and
// an environment select; the buttons update the message, "Feedback" opens a
// modal and its submit is answered privately.
// Run: MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... node examples/buttons.ts
import {
  actionRow,
  button,
  ButtonStyle,
  Client,
  Intents,
  linkButton,
  modal,
  stringSelect,
  textInput,
  TextInputStyle,
} from 'mnema-talk-sdk'

const client = new Client({
  baseUrl: process.env.MNEMA_URL ?? 'https://talk.example.org',
  token: process.env.MNEMA_TOKEN ?? '',
  intents: Intents.GuildMessages,
})

const environments = stringSelect({
  customId: 'env',
  placeholder: 'Environment',
  options: [
    { label: 'Staging', value: 'staging', default: true },
    { label: 'Production', value: 'prod' },
  ],
})

function deployRows(env: string) {
  return [
    actionRow(
      button({ customId: `deploy:${env}`, label: `Deploy to ${env}`, style: ButtonStyle.Success }),
      button({ customId: 'cancel', label: 'Cancel', style: ButtonStyle.Secondary }),
      button({ customId: 'feedback', label: 'Feedback', emoji: '📝' }),
      linkButton({ url: 'https://talk.example.org/changes', label: 'Changelog' }),
    ),
    actionRow(environments),
  ]
}

client.on('messageCreate', async (m) => {
  if (m.user_id === client.user?.id || m.content !== '!deploy') return
  await client.rest.createMessage(m.channel_id, { content: 'Deploy v2.4?', components: deployRows('staging') })
})

client.on('interactionCreate', async (i) => {
  if (i.isSelect() && i.customId === 'env') {
    const env = i.values[0] ?? 'staging'
    await i.update({ content: `Deploy v2.4 to ${env}?`, components: deployRows(env) })
  } else if (i.isButton() && i.customId.startsWith('deploy:')) {
    const env = i.customId.slice('deploy:'.length)
    await i.update({ content: `${i.user.display_name} deployed v2.4 to ${env}.`, components: [] })
  } else if (i.isButton() && i.customId === 'cancel') {
    await i.update({ content: 'Deploy cancelled.', components: [] })
  } else if (i.isButton() && i.customId === 'feedback') {
    await i.showModal(
      modal({
        customId: 'feedback-form',
        title: 'Feedback',
        components: [
          textInput({ customId: 'subject', label: 'Subject', maxLength: 80 }),
          textInput({ customId: 'body', label: 'What happened?', style: TextInputStyle.Paragraph, required: false }),
        ],
      }),
    )
  } else if (i.isModalSubmit() && i.customId === 'feedback-form') {
    await i.reply({ content: `Thanks! Noted: "${i.fields.get('subject')}"`, ephemeral: true })
  }
})

client.on('error', (err) => console.error(err))
process.on('SIGINT', () => void client.close().then(() => process.exit(0)))

await client.connect()
