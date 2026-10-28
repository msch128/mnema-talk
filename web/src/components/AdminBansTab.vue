<script setup lang="ts">
// Admin dashboard: active bans, with the reason, who banned and until when,
// and a way to lift each one.
import { ref, onMounted } from 'vue'
import { Gavel, Undo2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { AuthBan } from '../types/rest'
import { isAuthBan } from '../types/rest'
import { arrayDecoder } from '../types/validation'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'
import EmptyState from './EmptyState.vue'
import { useReloadOnActivate } from '../composables/useReloadOnActivate'
import StatePanel from './StatePanel.vue'

const toasts = useToastStore()
const bans = ref<AuthBan[]>([])
const loading = ref(false)
const failed = ref(false)

async function load() {
  loading.value = true
  try {
    bans.value = await api('/api/admin/bans', { decode: arrayDecoder('bans', isAuthBan) })
    failed.value = false
  } catch (e) {
    failed.value = true
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    loading.value = false
  }
}

async function unban(ban: AuthBan) {
  const name = ban.display_name || ban.username
  const ok = await confirm({ title: t('moderation.unbanTitle', { name }), body: t('moderation.unbanBody'), confirmLabel: t('moderation.unbanConfirm') })
  if (!ok) return
  try {
    await api(`/api/admin/users/${ban.user_id}/ban`, { method: 'DELETE' })
    bans.value = bans.value.filter(b => b.user_id !== ban.user_id)
    toasts.success(t('moderation.unbannedToast', { name }))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('moderation.failed')))
  }
}

function when(iso: string) {
  return new Date(iso).toLocaleString(locale.value)
}

useReloadOnActivate(load)
onMounted(load)
</script>

<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between">
      <h3 class="flex items-center gap-2 font-mono text-sm font-semibold uppercase tracking-wider text-mnema-text">
        <Gavel class="h-4 w-4 text-mnema-accent" />
        <span>{{ $t('moderation.bansTitle') }}</span>
      </h3>
    </div>

    <div class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <StatePanel v-if="failed && !bans.length" state="error" retryable data-testid="bans-error" @retry="load">{{ $t('moderation.bansLoadFailed') }}</StatePanel>
      <EmptyState v-else-if="!bans.length && !loading" :icon="Gavel" :text="$t('moderation.noBans')" :hint="$t('moderation.noBansHint')" data-testid="bans-empty" />
      <div v-for="ban in bans" :key="ban.user_id" class="flex items-center justify-between gap-3 px-4 py-2.5 text-sm" :data-testid="`ban-${ban.user_id}`">
        <div class="min-w-0 space-y-0.5">
          <div class="flex min-w-0 items-center gap-2">
            <span class="truncate font-semibold text-mnema-text">{{ ban.display_name || ban.username }}</span>
            <span class="truncate font-mono text-xs text-mnema-tertiary">@{{ ban.username }}</span>
          </div>
          <div class="text-xs text-mnema-muted">
            {{ ban.banned_until ? $t('moderation.bannedUntil', { date: when(ban.banned_until) }) : $t('moderation.bannedForever') }}
            <span v-if="ban.banned_by"> · {{ $t('moderation.bannedBy', { name: ban.banned_by }) }}</span>
          </div>
          <div v-if="ban.reason" class="wrap-break-word text-xs text-mnema-tertiary">{{ ban.reason }}</div>
        </div>
        <button
          type="button"
          data-testid="unban"
          class="flex shrink-0 items-center gap-1.5 font-medium text-mnema-accent transition hover:text-mnema-accent-hover"
          @click="unban(ban)"
        >
          <Undo2 class="h-4 w-4" />
          <span>{{ $t('moderation.unban') }}</span>
        </button>
      </div>
    </div>
  </section>
</template>
