<script setup lang="ts">
// Integrations of one channel: incoming and outgoing webhooks. Shown to those
// who hold Manage webhooks there; the server checks again on every call.
import type { PropType } from 'vue'
import { ref } from 'vue'
import BaseDialog from './BaseDialog.vue'
import IncomingWebhooksPanel from './IncomingWebhooksPanel.vue'
import OutgoingWebhooksPanel from './OutgoingWebhooksPanel.vue'

defineProps({
  channel: { type: Object as PropType<{ id: string; name: string }>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const tab = ref<'incoming' | 'outgoing'>('incoming')
</script>

<template>
  <BaseDialog :title="$t('webhooks.title', { name: channel.name })" panel-class="max-w-2xl max-h-[90vh]" @close="emit('close')">
    <div class="flex min-h-0 flex-1 flex-col">
      <div role="tablist" :aria-label="$t('webhooks.tabs')" class="flex gap-1 border-b border-mnema-hairline px-5 pt-3">
        <button
          v-for="id in (['incoming', 'outgoing'] as const)"
          :key="id"
          type="button"
          role="tab"
          :aria-selected="tab === id"
          :class="['-mb-px rounded-t-md border-b-2 px-3 py-1.5 text-sm transition', tab === id ? 'border-mnema-accent font-semibold text-mnema-text' : 'border-transparent text-mnema-muted hover:text-mnema-text']"
          @click="tab = id"
        >{{ $t(`webhooks.tab.${id}`) }}</button>
      </div>
      <div class="flex-1 overflow-y-auto p-5" role="tabpanel">
        <IncomingWebhooksPanel v-if="tab === 'incoming'" :channel-id="channel.id" />
        <OutgoingWebhooksPanel v-else :channel-id="channel.id" />
      </div>
    </div>
  </BaseDialog>
</template>
