<script setup lang="ts">
// Admin dashboard: the audit log. Read-only by design: entries are immutable
// on the server, so there is nothing to edit or delete here.
import { ref, computed, onMounted } from 'vue'
import { ScrollText, ChevronRight, ChevronDown } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { AuditItem, AuditPage } from '../types/rest'
import { isAuditPage } from '../types/rest'
import { decoder } from '../types/validation'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { t, locale } from '../i18n'
import BotBadge from './BotBadge.vue'

const decodePage = decoder<AuditPage>('audit.Page', isAuditPage)
const toasts = useToastStore()
const chat = useChatStore()

// Event groups the server understands as a prefix (action=role matches role.*).
const GROUPS = ['role', 'user', 'channel', 'category', 'layout', 'invite', 'webhook', 'outgoing_webhook', 'media', 'settings', 'moderation', 'message', 'voice', 'auth', 'bot', 'emoji', 'system'] as const

const entries = ref<AuditItem[]>([])
const next = ref<number | null>(null)
const loading = ref(false)
const group = ref('')
const actor = ref('')
const from = ref('')
const to = ref('')
const open = ref<Set<number>>(new Set())

// Bots appear in the log by name too; the member list says who is one.
const botIds = computed(() => new Set(chat.members.filter(m => m.is_bot).map(m => m.id)))
const actors = computed(() => chat.members.map(m => ({ id: m.id, name: m.display_name || m.username })))

function query(before: number | null): string {
  const q = new URLSearchParams({ limit: '50' })
  if (group.value) q.set('action', group.value)
  if (actor.value) q.set('actor', actor.value)
  if (from.value) q.set('from', from.value)
  if (to.value) q.set('to', to.value)
  if (before !== null) q.set('before', String(before))
  return q.toString()
}

async function load(more: boolean) {
  if (loading.value) return
  loading.value = true
  try {
    const page = await api(`/api/admin/audit-log?${query(more ? next.value : null)}`, { decode: decodePage })
    entries.value = more ? [...entries.value, ...page.entries] : page.entries
    next.value = page.next_before
    if (!more) open.value = new Set()
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    loading.value = false
  }
}

function toggle(id: number) {
  const s = new Set(open.value)
  if (!s.delete(id)) s.add(id)
  open.value = s
}

function actionLabel(action: string): string {
  const key = `audit.actions.${action}`
  const label = t(key)
  return label === key ? action : label
}

function when(iso: string): string {
  return new Date(iso).toLocaleString(locale.value)
}

function pretty(details: Record<string, unknown>): string {
  return JSON.stringify(details, null, 2)
}

onMounted(() => load(false))
</script>

<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between">
      <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
        <ScrollText class="w-4 h-4 text-mnema-accent" />
        <span>{{ $t('audit.title') }}</span>
      </h3>
      <span class="text-xs text-mnema-tertiary">{{ $t('audit.immutable') }}</span>
    </div>

    <form class="flex flex-wrap gap-2" data-testid="audit-filters" @submit.prevent="load(false)">
      <select
        v-model="group"
        :aria-label="$t('audit.filterEvent')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text outline-none min-w-0 flex-1 basis-40 focus:border-mnema-accent transition"
      >
        <option value="">{{ $t('audit.allEvents') }}</option>
        <option v-for="g in GROUPS" :key="g" :value="g">{{ $t(`audit.groups.${g}`) }}</option>
      </select>
      <select
        v-model="actor"
        :aria-label="$t('audit.filterActor')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text outline-none min-w-0 flex-1 basis-40 focus:border-mnema-accent transition"
      >
        <option value="">{{ $t('audit.allActors') }}</option>
        <option v-for="a in actors" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
      <input
        v-model="from"
        type="date"
        :aria-label="$t('audit.from')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text outline-none min-w-0 flex-1 basis-36 focus:border-mnema-accent transition"
      />
      <input
        v-model="to"
        type="date"
        :aria-label="$t('audit.to')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text outline-none min-w-0 flex-1 basis-36 focus:border-mnema-accent transition"
      />
      <button
        type="submit"
        class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-3.5 py-1.5 rounded-md text-sm transition shadow-sm"
      >
        {{ $t('audit.apply') }}
      </button>
    </form>

    <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
      <div v-if="!entries.length && !loading" class="p-4 text-sm text-mnema-tertiary">{{ $t('audit.empty') }}</div>
      <div v-for="e in entries" :key="e.id" :data-testid="`audit-entry-${e.id}`">
        <button
          type="button"
          class="w-full px-4 py-2.5 flex items-center gap-3 text-sm text-left hover:bg-mnema-elevated/50 transition"
          :aria-expanded="open.has(e.id) ? 'true' : 'false'"
          @click="toggle(e.id)"
        >
          <ChevronDown v-if="open.has(e.id)" class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
          <ChevronRight v-else class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
          <span class="text-mnema-tertiary whitespace-nowrap font-mono text-xs">{{ when(e.created_at) }}</span>
          <span class="text-mnema-text font-medium truncate">{{ e.actor_name || $t('audit.system') }}</span>
          <BotBadge v-if="e.actor_id && botIds.has(e.actor_id)" />
          <span class="text-mnema-accent truncate">{{ actionLabel(e.action) }}</span>
          <span v-if="e.target_name || e.target_id" class="text-mnema-muted truncate">{{ e.target_name || e.target_id }}</span>
          <BotBadge v-if="e.target_id && botIds.has(e.target_id)" />
        </button>
        <div v-if="open.has(e.id)" class="px-4 pb-3 space-y-1.5">
          <div class="text-xs text-mnema-tertiary font-mono break-all">
            {{ e.action }} · {{ e.target_type }} {{ e.target_id }} · {{ $t('audit.request') }} {{ e.request_id }}
          </div>
          <pre v-if="Object.keys(e.details).length" class="bg-mnema-canvas border border-mnema-border rounded-md p-3 text-xs text-mnema-text overflow-x-auto whitespace-pre-wrap break-words">{{ pretty(e.details) }}</pre>
        </div>
      </div>
    </div>

    <div v-if="next !== null" class="flex justify-center">
      <button
        type="button"
        :disabled="loading"
        class="px-3.5 py-1.5 rounded-md text-sm border border-mnema-border text-mnema-muted hover:text-mnema-text transition disabled:opacity-50"
        @click="load(true)"
      >
        {{ $t('audit.loadMore') }}
      </button>
    </div>
  </section>
</template>
