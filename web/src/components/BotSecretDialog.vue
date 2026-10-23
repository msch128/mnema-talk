<script setup lang="ts">
// A bot token is shown exactly once, right after issuing or regenerating it.
import { computed } from 'vue'
import { Copy, TriangleAlert } from '@lucide/vue'
import { curlExample } from '../lib/bots'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

defineProps({
  secret: { type: String, required: true },
  botName: { type: String, required: true }
})
const emit = defineEmits<{ close: [] }>()
const toasts = useToastStore()
const curl = computed(() => curlExample(window.location.origin))

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toasts.success(t('bots.secret.copied'))
  } catch {
    toasts.error(t('admin.copyFailed'))
  }
}
</script>

<template>
  <BaseDialog :title="$t('bots.secret.title', { name: botName })" panel-class="max-w-lg" @close="emit('close')">
    <div class="space-y-4 p-5" data-testid="bot-secret">
      <p class="flex items-start gap-2 rounded-md border border-mnema-danger/40 bg-mnema-danger/10 px-3 py-2 text-sm text-mnema-text" role="alert">
        <TriangleAlert class="mt-0.5 h-4 w-4 shrink-0 text-mnema-danger" aria-hidden="true" />
        <span>{{ $t('bots.secret.once') }}</span>
      </p>
      <div class="flex items-center gap-2">
        <code class="min-w-0 flex-1 select-all break-all rounded-sm bg-mnema-surface px-2 py-1.5 font-mono text-xs text-mnema-text" data-testid="bot-secret-value">{{ secret }}</code>
        <button type="button" class="btn btn-secondary p-2" :aria-label="$t('bots.secret.copy')" data-testid="bot-secret-copy" @click="copy(secret)">
          <Copy class="h-4 w-4" />
        </button>
      </div>
      <div class="space-y-1">
        <p class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.secret.try') }}</p>
        <pre class="overflow-x-auto rounded-sm bg-mnema-surface px-2 py-1.5 font-mono text-xs text-mnema-text" data-testid="bot-secret-curl">{{ curl }}</pre>
      </div>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" data-testid="bot-secret-done" @click="emit('close')">{{ $t('bots.secret.done') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>
