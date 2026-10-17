// Slash commands in the channel composer: the list that opens while "/" is
// typed, the picked command with its option fields, and running it. The
// commands of a channel are those of bots that can view and send there; the
// server decides (GET /api/channels/{id}/commands), the composer only offers.
import { ref, computed, reactive } from 'vue'
import type { Ref } from 'vue'
import { api, isApiError, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isBotChannelCommand, decodeBotInvoked } from '../types/rest'
import type { BotChannelCommand } from '../types/rest'
import { slashQuery, filterCommands, emptyValues, buildOptions } from '../lib/slashCommands'
import type { OptionValues } from '../lib/slashCommands'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

/** How long the commands of a channel are reused before they are asked for again. */
const FRESH_MS = 60_000

export function useSlashCommands(model: Ref<string>, channelId: () => string | undefined) {
  const toasts = useToastStore()
  const commands = ref<BotChannelCommand[]>([])
  let loadedFor = ''
  let loadedAt = 0
  const query = ref<string | null>(null)
  const active = ref(0)
  const picked = ref<BotChannelCommand | null>(null)
  const values = reactive<OptionValues>({})
  const running = ref(false)
  const dismissed = ref(false)

  const suggestions = computed(() => (query.value === null || dismissed.value ? [] : filterCommands(commands.value, query.value)))
  const open = computed(() => suggestions.value.length > 0)

  async function load(id: string) {
    if (loadedFor === id && Date.now() - loadedAt < FRESH_MS) return
    loadedFor = id
    loadedAt = Date.now()
    try {
      const list = await api(`/api/channels/${id}/commands`, { decode: arrayDecoder('channel commands', isBotChannelCommand) })
      if (loadedFor === id) commands.value = list
    } catch {
      // No list is no menu; typing "/" stays an ordinary message.
      loadedAt = 0
      commands.value = []
    }
  }

  /** Call from the textarea's input handler. */
  function onInput() {
    const next = slashQuery(model.value)
    if (next !== query.value) {
      active.value = 0
      dismissed.value = false
    }
    query.value = next
    const id = channelId()
    if (next !== null && id) void load(id)
  }

  function cancel() {
    picked.value = null
    for (const k of Object.keys(values)) delete values[k]
  }

  function pick(command: BotChannelCommand | undefined) {
    if (!command) return
    cancel()
    Object.assign(values, emptyValues(command))
    picked.value = command
    model.value = ''
    query.value = null
  }

  async function run() {
    const command = picked.value
    const id = channelId()
    if (!command || !id || running.value) return false
    const built = buildOptions(command, values)
    if (built.problem) {
      toasts.error(t(built.problem.kind === 'missing' ? 'slash.missing' : 'slash.notANumber', { name: built.problem.name }))
      return false
    }
    running.value = true
    try {
      await api(`/api/channels/${id}/interactions`, { method: 'POST', json: { command_id: command.id, options: built.options }, decode: decodeBotInvoked })
      cancel()
      return true
    } catch (err) {
      toasts.error(isApiError(err) && err.status === 409 ? t('slash.botOffline', { bot: command.bot_name }) : caughtErrorMessage(err, t('slash.failed')))
      return false
    } finally {
      running.value = false
    }
  }

  /** Handles keys while the list or a picked command is up; true if consumed. */
  function onKeydown(e: KeyboardEvent) {
    if (open.value) {
      const n = suggestions.value.length
      if (e.key === 'ArrowDown') { e.preventDefault(); active.value = (active.value + 1) % n; return true }
      if (e.key === 'ArrowUp') { e.preventDefault(); active.value = (active.value + n - 1) % n; return true }
      if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(suggestions.value[active.value]); return true }
      if (e.key === 'Escape') { e.preventDefault(); dismissed.value = true; return true }
      return false
    }
    if (picked.value) {
      if (e.key === 'Escape') { e.preventDefault(); cancel(); return true }
      if (e.key === 'Enter' && !e.shiftKey && !model.value.trim()) { e.preventDefault(); void run(); return true }
    }
    return false
  }

  function close() {
    dismissed.value = true
  }

  return { commands, suggestions, open, active, picked, values, running, onInput, onKeydown, pick, run, cancel, close }
}
