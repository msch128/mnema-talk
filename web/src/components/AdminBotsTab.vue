<script setup lang="ts">
// Admin dashboard: bots. A list on the left, the selected bot's settings on
// the right. The server checks every rule; this only offers what fits.
import { useReloadOnActivate } from '../composables/useReloadOnActivate'
import { ref, computed, onMounted } from 'vue'
import { Bot, Plus } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { decodeBotBot, isBotBot } from '../types/rest'
import type { BotBot } from '../types/rest'
import { botInitial, relativeTime } from '../lib/bots'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'
import BotDetailPanel from './BotDetailPanel.vue'
import EmptyState from './EmptyState.vue'

const permissions = usePermissionsStore()
const toasts = useToastStore()
const bots = ref<BotBot[]>([])
const loaded = ref(false)
const selectedId = ref<string | null>(null)
const newName = ref('')
const busy = ref(false)

const selected = computed(() => bots.value.find(b => b.id === selectedId.value) ?? null)

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}
function roleNames(bot: BotBot): string[] {
  return bot.role_ids.map(id => permissions.roleById(id)?.name).filter((n): n is string => !!n)
}

async function load() {
  try {
    bots.value = await api('/api/admin/bots', { decode: arrayDecoder('bots', isBotBot) })
  } catch (e) {
    fail(e)
  } finally {
    loaded.value = true
  }
}

async function create() {
  const name = newName.value.trim()
  if (!name || busy.value) return
  busy.value = true
  try {
    const bot = await api('/api/admin/bots', { method: 'POST', json: { name }, decode: decodeBotBot })
    bots.value = [...bots.value, bot]
    selectedId.value = bot.id
    newName.value = ''
    toasts.success(t('bots.created'))
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

function onUpdated(bot: BotBot) {
  bots.value = bots.value.map(b => (b.id === bot.id ? bot : b))
}
function onDeleted(id: string) {
  bots.value = bots.value.filter(b => b.id !== id)
  if (selectedId.value === id) selectedId.value = null
}

useReloadOnActivate(load)
onMounted(async () => {
  await Promise.all([load(), permissions.loadRoles()])
})
</script>

<template>
  <section class="space-y-4" data-testid="admin-bots">
    <h3 class="flex items-center gap-2 font-mono text-sm font-semibold uppercase tracking-wider text-mnema-text">
      <Bot class="h-4 w-4 text-mnema-accent" aria-hidden="true" />
      <span>{{ $t('bots.title') }}</span>
    </h3>
    <p class="text-sm text-mnema-tertiary">{{ $t('bots.intro') }}</p>

    <div class="grid gap-4 md:grid-cols-[minmax(0,18rem)_minmax(0,1fr)]">
      <div class="space-y-3">
        <form class="flex gap-2" @submit.prevent="create">
          <input v-model="newName" type="text" maxlength="24" class="min-w-0 flex-1 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" :placeholder="$t('bots.namePlaceholder')" :aria-label="$t('common.name')" data-testid="bot-new-name" />
          <button type="submit" :disabled="busy || !newName.trim()" class="btn btn-primary flex items-center gap-1.5" data-testid="bot-create">
            <Plus class="h-4 w-4" aria-hidden="true" />
            <span>{{ $t('bots.new') }}</span>
          </button>
        </form>
        <EmptyState v-if="loaded && !bots.length" :icon="Bot" :text="$t('bots.empty')" :hint="$t('bots.emptyHint')" class="rounded-lg border border-mnema-hairline bg-mnema-surface" />
        <ul v-if="bots.length" :aria-label="$t('bots.list')" class="overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
          <li v-for="bot in bots" :key="bot.id" :class="['border-b border-mnema-hairline last:border-b-0', selectedId === bot.id ? 'bg-mnema-accent-subtle' : '']" data-testid="bot-row">
            <button type="button" class="flex w-full items-start gap-3 p-3 text-left" :aria-pressed="selectedId === bot.id" @click="selectedId = bot.id">
              <img v-if="bot.avatar_url" :src="bot.avatar_url" alt="" class="h-9 w-9 shrink-0 rounded-full object-cover" />
              <span v-else class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-mnema-raised text-sm font-semibold text-mnema-muted" aria-hidden="true">{{ botInitial(bot.name) }}</span>
              <span class="min-w-0 flex-1 space-y-0.5">
                <span class="flex items-center gap-1.5">
                  <span class="truncate text-sm font-medium text-mnema-text">{{ bot.name }}</span>
                  <span class="rounded-sm bg-mnema-accent/15 px-1 text-[10px] font-semibold uppercase text-mnema-accent">{{ $t('bots.badge') }}</span>
                </span>
                <span class="block text-xs" :class="bot.disabled ? 'text-mnema-danger' : 'text-mnema-tertiary'" data-testid="bot-state">{{ bot.disabled ? $t('bots.disabled') : $t('bots.enabled') }} - {{ bot.last_active_at ? $t('bots.lastActive', { when: relativeTime(bot.last_active_at, locale) }) : $t('bots.neverActive') }}</span>
                <span v-if="roleNames(bot).length" class="flex flex-wrap gap-1 pt-0.5">
                  <span v-for="role in roleNames(bot)" :key="role" class="rounded-full bg-mnema-raised px-1.5 py-0.5 text-[11px] text-mnema-muted">{{ role }}</span>
                </span>
                <span v-else class="block text-xs text-mnema-tertiary">{{ $t('bots.noRoles') }}</span>
              </span>
            </button>
          </li>
        </ul>
      </div>

      <BotDetailPanel v-if="selected" :key="selected.id" :bot="selected" @updated="onUpdated" @deleted="onDeleted" />
      <p v-else class="text-sm text-mnema-tertiary">{{ $t('bots.select') }}</p>
    </div>
  </section>
</template>
