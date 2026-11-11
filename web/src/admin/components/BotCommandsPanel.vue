<script setup lang="ts">
// Slash commands a bot registered: turn off or on, or delete. These are actions
// that run when pressed (doc/settings-ux.md), not fields of the bot's draft, so
// they are buttons with a toast rather than switches. Members see a command in
// channels where the bot can view and send.
import { ref, onMounted } from 'vue'
import { SquareSlash, Trash2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../../shared/lib/api'
import { arrayDecoder } from '../../types/validation'
import { decodeBotCommand, isBotCommand } from '../../types/rest'
import type { BotCommand } from '../../types/rest'
import { confirm } from '../../shared/lib/confirm'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'

const props = defineProps({
  botId: { type: String, required: true }
})

const toasts = useToastStore()
const commands = ref<BotCommand[]>([])
const loaded = ref(false)
const busy = ref('')
const base = `/api/admin/bots/${props.botId}/commands`

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

async function load() {
  try {
    commands.value = await api(base, { decode: arrayDecoder('bot commands', isBotCommand) })
  } catch (e) {
    fail(e)
  } finally {
    loaded.value = true
  }
}

async function toggle(command: BotCommand) {
  const disabled = !command.disabled
  busy.value = command.id
  try {
    const updated = await api(`${base}/${command.id}`, { method: 'PATCH', json: { disabled }, decode: decodeBotCommand })
    commands.value = commands.value.map(c => (c.id === updated.id ? updated : c))
    toasts.success(t(updated.disabled ? 'bots.commands.turnedOff' : 'bots.commands.turnedOn', { name: updated.name }))
  } catch (e) {
    fail(e)
  } finally {
    busy.value = ''
  }
}

async function remove(command: BotCommand) {
  const ok = await confirm({ title: t('bots.commands.deleteTitle', { name: command.name }), body: t('bots.commands.deleteBody'), confirmLabel: t('common.delete'), danger: true })
  if (!ok) return
  try {
    await api(`${base}/${command.id}`, { method: 'DELETE' })
    commands.value = commands.value.filter(c => c.id !== command.id)
    toasts.success(t('bots.commands.deleted', { name: command.name }))
  } catch (e) {
    fail(e)
  }
}

onMounted(load)
</script>

<template>
  <section class="space-y-3" data-testid="bot-commands">
    <h4 class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary"><SquareSlash class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('bots.commands.title') }}</h4>
    <p class="text-sm text-mnema-tertiary">{{ $t('bots.commands.hint') }}</p>
    <p v-if="loaded && !commands.length" class="text-sm text-mnema-tertiary">{{ $t('bots.commands.empty') }}</p>
    <ul v-if="commands.length" class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <li v-for="command in commands" :key="command.id" class="flex flex-wrap items-center gap-3 p-3" data-testid="bot-command">
        <div class="min-w-0 flex-1">
          <p class="truncate font-mono text-sm text-mnema-text">/{{ command.name }}</p>
          <p class="truncate text-xs text-mnema-tertiary">{{ command.description }} <span class="whitespace-nowrap">- {{ $t('bots.commands.options', { count: command.options.length }) }}</span></p>
        </div>
        <span :class="['text-xs', command.disabled ? 'text-mnema-tertiary' : 'text-mnema-accent']" data-testid="bot-command-state">{{ command.disabled ? $t('bots.disabled') : $t('bots.enabled') }}</span>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="busy === command.id"
          :aria-label="$t(command.disabled ? 'bots.commands.turnOnFor' : 'bots.commands.turnOffFor', { name: command.name })"
          data-action="toggle"
          @click="toggle(command)"
        >{{ command.disabled ? $t('bots.commands.turnOn') : $t('bots.commands.turnOff') }}</button>
        <button type="button" class="btn btn-icon btn-sm hover:text-mnema-danger" :aria-label="$t('bots.commands.deleteFor', { name: command.name })" data-action="delete" @click="remove(command)"><Trash2 class="h-4 w-4" /></button>
      </li>
    </ul>
  </section>
</template>
