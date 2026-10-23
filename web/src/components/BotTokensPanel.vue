<script setup lang="ts">
// Tokens of one bot: a table, issuing with an optional name and expiry, and
// regenerate / revoke. The secret itself is only ever shown by the dialog.
import { ref, onMounted } from 'vue'
import { KeyRound, RefreshCw, Ban } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { decodeBotNewTokenResponse, isBotToken } from '../types/rest'
import type { BotToken } from '../types/rest'
import { confirm } from '../lib/confirm'
import { endOfDayIso } from '../lib/bots'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'
import BotSecretDialog from './BotSecretDialog.vue'

const props = defineProps({
  botId: { type: String, required: true },
  botName: { type: String, required: true }
})

const toasts = useToastStore()
const tokens = ref<BotToken[]>([])
const loaded = ref(false)
const busy = ref(false)
const name = ref('')
const expires = ref('')
const secret = ref<string | null>(null)
const base = `/api/admin/bots/${props.botId}/tokens`

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}
function day(iso: string | undefined): string {
  return iso ? new Date(iso).toLocaleDateString(locale.value) : '-'
}
function label(token: BotToken): string {
  return token.name || `...${token.hint}`
}

async function load() {
  try {
    tokens.value = await api(base, { decode: arrayDecoder('bot tokens', isBotToken) })
  } catch (e) {
    fail(e)
  } finally {
    loaded.value = true
  }
}

async function issue() {
  if (busy.value) return
  const json: { name?: string; expires_at?: string } = {}
  if (name.value.trim()) json.name = name.value.trim()
  const until = endOfDayIso(expires.value)
  if (until) json.expires_at = until
  busy.value = true
  try {
    const created = await api(base, { method: 'POST', json, decode: decodeBotNewTokenResponse })
    secret.value = created.secret
    name.value = ''
    expires.value = ''
    await load()
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function regenerate(token: BotToken) {
  const ok = await confirm({ title: t('bots.tokens.regenerateTitle', { name: label(token) }), body: t('bots.tokens.regenerateBody'), confirmLabel: t('bots.tokens.regenerate'), danger: true })
  if (!ok) return
  try {
    const created = await api(`${base}/${token.id}/regenerate`, { method: 'POST', decode: decodeBotNewTokenResponse })
    secret.value = created.secret
    await load()
  } catch (e) {
    fail(e)
  }
}

async function revoke(token: BotToken) {
  const ok = await confirm({ title: t('bots.tokens.revokeTitle', { name: label(token) }), body: t('bots.tokens.revokeBody'), confirmLabel: t('bots.tokens.revoke'), danger: true })
  if (!ok) return
  try {
    await api(`${base}/${token.id}`, { method: 'DELETE' })
    await load()
  } catch (e) {
    fail(e)
  }
}

onMounted(load)
</script>

<template>
  <section class="space-y-3" data-testid="bot-tokens">
    <h4 class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary"><KeyRound class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('bots.tokens.title') }}</h4>
    <p class="text-sm text-mnema-tertiary">{{ $t('bots.tokens.hint') }}</p>

    <form class="flex flex-wrap items-end gap-2" @submit.prevent="issue">
      <label class="min-w-0 flex-1 stack-1">
        <span class="text-xs text-mnema-tertiary">{{ $t('bots.tokens.name') }}</span>
        <input v-model="name" type="text" maxlength="32" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" :placeholder="$t('bots.tokens.namePlaceholder')" />
      </label>
      <label class="stack-1">
        <span class="text-xs text-mnema-tertiary">{{ $t('bots.tokens.expiry') }}</span>
        <input v-model="expires" type="date" class="rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1.5 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
      </label>
      <button type="submit" :disabled="busy" class="rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40">{{ $t('bots.tokens.issue') }}</button>
    </form>

    <p v-if="loaded && !tokens.length" class="text-sm text-mnema-tertiary">{{ $t('bots.tokens.empty') }}</p>
    <div v-if="tokens.length" class="overflow-x-auto rounded-lg border border-mnema-hairline bg-mnema-surface">
      <table class="w-full text-left text-sm">
        <thead class="text-xs uppercase tracking-wide text-mnema-tertiary">
          <tr>
            <th scope="col" class="px-3 py-2 font-semibold">{{ $t('common.name') }}</th>
            <th scope="col" class="px-3 py-2 font-semibold">{{ $t('bots.tokens.colToken') }}</th>
            <th scope="col" class="px-3 py-2 font-semibold">{{ $t('bots.tokens.colState') }}</th>
            <th scope="col" class="px-3 py-2 font-semibold">{{ $t('bots.tokens.colExpires') }}</th>
            <th scope="col" class="px-3 py-2 font-semibold">{{ $t('bots.tokens.colLastUsed') }}</th>
            <th scope="col" class="px-3 py-2"><span class="sr-only">{{ $t('bots.tokens.colActions') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="token in tokens" :key="token.id" class="border-t border-mnema-hairline" data-testid="bot-token">
            <td class="px-3 py-2 text-mnema-text">{{ token.name || '-' }}</td>
            <td class="px-3 py-2 font-mono text-xs text-mnema-muted">...{{ token.hint }}</td>
            <td class="px-3 py-2"><span :class="token.state === 'active' ? 'text-mnema-accent' : 'text-mnema-tertiary'" data-testid="bot-token-state">{{ $t(`bots.tokens.state.${token.state}`) }}</span>
              <span v-if="token.issuer_binding" class="ml-2 rounded-full border border-mnema-hairline px-2 py-0.5 text-xs text-mnema-tertiary" :title="$t(`bots.tokens.binding.${token.issuer_binding}Hint`)" data-testid="bot-token-binding">{{ $t(`bots.tokens.binding.${token.issuer_binding}`) }}</span></td>
            <td class="px-3 py-2 text-mnema-muted">{{ day(token.expires_at) }}</td>
            <td class="px-3 py-2 text-mnema-muted">{{ token.last_used_at ? day(token.last_used_at) : $t('bots.tokens.never') }}</td>
            <td class="px-3 py-2">
              <div class="flex justify-end gap-1 text-mnema-tertiary">
                <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-text" :aria-label="$t('bots.tokens.regenerateFor', { name: label(token) })" data-action="regenerate" @click="regenerate(token)"><RefreshCw class="h-4 w-4" /></button>
                <button type="button" :disabled="token.state === 'revoked'" class="rounded-sm p-1.5 transition hover:text-mnema-danger disabled:opacity-30" :aria-label="$t('bots.tokens.revokeFor', { name: label(token) })" data-action="revoke" @click="revoke(token)"><Ban class="h-4 w-4" /></button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BotSecretDialog v-if="secret" :secret="secret" :bot-name="botName" @close="secret = null" />
  </section>
</template>
