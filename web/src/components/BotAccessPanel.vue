<script setup lang="ts">
// Read-only preview of what a bot can do: the effective result of its roles
// and the channel overwrites. Reloads when `version` changes (roles saved).
import { ref, watch, onMounted } from 'vue'
import { Eye, TriangleAlert } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeBotAccess } from '../types/rest'
import type { BotAccess } from '../types/rest'
import { PERMISSION_BITS, PERMISSION_KEYS } from '../lib/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const props = defineProps({
  botId: { type: String, required: true },
  version: { type: Number, default: 0 }
})

const toasts = useToastStore()
const access = ref<BotAccess | null>(null)

function keysOf(bits: number) {
  return PERMISSION_KEYS.filter(key => (bits & PERMISSION_BITS[key]) === PERMISSION_BITS[key])
}

async function load() {
  try {
    access.value = await api(`/api/admin/bots/${props.botId}/access`, { decode: decodeBotAccess })
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

watch(() => props.version, load)
onMounted(load)
</script>

<template>
  <section class="space-y-3" data-testid="bot-access">
    <h4 class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary"><Eye class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('bots.access.title') }}</h4>
    <template v-if="access">
      <p v-if="access.elevated" class="flex items-start gap-2 rounded-md border border-mnema-danger/40 bg-mnema-danger/10 px-3 py-2 text-sm text-mnema-text" role="alert" data-testid="bot-elevated">
        <TriangleAlert class="mt-0.5 h-4 w-4 shrink-0 text-mnema-danger" aria-hidden="true" />
        <span>{{ $t('bots.access.elevated') }}</span>
      </p>
      <div class="space-y-1">
        <p class="text-xs text-mnema-tertiary">{{ $t('bots.access.server') }}</p>
        <p v-if="!keysOf(access.permissions).length" class="text-sm text-mnema-tertiary" data-testid="bot-access-none">{{ $t('bots.access.none') }}</p>
        <ul v-else class="flex flex-wrap gap-1.5" :aria-label="$t('bots.access.server')">
          <li v-for="key in keysOf(access.permissions)" :key="key" class="rounded-full border border-mnema-border-strong px-2 py-0.5 text-xs text-mnema-text">{{ $t(`perm.${key}.name`) }}</li>
        </ul>
      </div>
      <p v-if="!access.channels.length" class="text-sm text-mnema-tertiary">{{ $t('bots.access.noChannels') }}</p>
      <ul v-else class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
        <li v-for="channel in access.channels" :key="channel.channel_id" class="space-y-1 p-3" data-testid="bot-access-channel">
          <p class="text-sm font-medium text-mnema-text"># {{ channel.name }}</p>
          <p v-if="!keysOf(channel.permissions).length" class="text-xs text-mnema-tertiary">{{ $t('bots.access.none') }}</p>
          <ul v-else class="flex flex-wrap gap-1.5">
            <li v-for="key in keysOf(channel.permissions)" :key="key" class="rounded-full bg-mnema-raised px-2 py-0.5 text-xs text-mnema-muted">{{ $t(`perm.${key}.name`) }}</li>
          </ul>
        </li>
      </ul>
    </template>
  </section>
</template>
