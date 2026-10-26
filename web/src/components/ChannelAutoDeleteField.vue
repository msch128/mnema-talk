<script setup lang="ts">
import type { PropType } from 'vue'
import { computed, ref } from 'vue'
import type { Channel } from '../types/domain'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { AUTO_DELETE_DAYS } from '../lib/autoDelete'
import { t } from '../i18n'

// Opt-in auto-delete of a channel, in the channel settings. It saves by itself
// (with its own button) because turning it on destroys data and asks first.
const props = defineProps({
  channel: { type: Object as PropType<Channel>, required: true }
})

const chat = useChatStore()
const features = useServerFeaturesStore()
const toasts = useToastStore()

const current = computed(() => props.channel.auto_delete_days ?? 0)
const chosen = ref(current.value)
const saving = ref(false)
const changed = computed(() => chosen.value !== current.value)

async function apply() {
  if (!changed.value || saving.value) return
  if (chosen.value > 0) {
    const ok = await confirm({
      title: t('autoDelete.confirmTitle'),
      body: t('autoDelete.confirmBody', { name: props.channel.name, period: t(`autoDelete.periods.${chosen.value}`) }),
      confirmLabel: t('autoDelete.confirmAction'),
      danger: true
    })
    if (!ok) return
  }
  saving.value = true
  try {
    await chat.setChannelAutoDelete(props.channel.id, chosen.value)
    toasts.success(t('autoDelete.saved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('autoDelete.failed')))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="features.channelAutoDelete" class="stack-1.5" data-testid="auto-delete-field">
    <label for="edit-auto-delete" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('autoDelete.label') }}
    </label>
    <div class="flex items-center gap-2">
      <select
        id="edit-auto-delete"
        v-model.number="chosen"
        data-testid="auto-delete-select"
        class="min-w-0 flex-1 bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
      >
        <option :value="0">{{ $t('autoDelete.off') }}</option>
        <option v-for="d in AUTO_DELETE_DAYS" :key="d" :value="d">{{ $t(`autoDelete.choices.${d}`) }}</option>
      </select>
      <button
        type="button"
        data-testid="auto-delete-apply"
        class="btn btn-secondary h-9 shrink-0"
        :disabled="!changed || saving"
        @click="apply"
      >{{ $t('autoDelete.apply') }}</button>
    </div>
    <p class="text-xs text-mnema-tertiary">{{ $t('autoDelete.hint') }}</p>
  </div>
</template>
