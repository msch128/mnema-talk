<script setup lang="ts">
// Integrations of one channel: incoming and outgoing webhooks. Shown to those
// who hold Manage webhooks there; the server checks again on every call.
import type { PropType } from 'vue'
import { ref } from 'vue'
import { rovingKeydown } from '../lib/rovingFocus'
import BaseDialog from './BaseDialog.vue'
import IncomingWebhooksPanel from './IncomingWebhooksPanel.vue'
import OutgoingWebhooksPanel from './OutgoingWebhooksPanel.vue'

defineProps({
  channel: { type: Object as PropType<{ id: string; name: string }>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const TABS = ['incoming', 'outgoing'] as const
const tab = ref<(typeof TABS)[number]>('incoming')
</script>

<template>
  <BaseDialog :title="$t('webhooks.title', { name: channel.name })" panel-class="max-w-2xl max-h-[90vh]" @close="emit('close')">
    <div class="flex min-h-0 flex-1 flex-col">
      <div role="tablist" :aria-label="$t('webhooks.tabs')" class="flex gap-1 border-b border-mnema-hairline px-5 pt-3">
        <button
          v-for="(id, i) in TABS"
          :key="id"
          type="button"
          role="tab"
          :id="`webhooks-tab-${id}`"
          :aria-selected="tab === id"
          :aria-controls="`webhooks-panel-${id}`"
          :tabindex="tab === id ? 0 : -1"
          :class="['-mb-px rounded-t-md border-b-2 px-3 py-1.5 text-sm transition', tab === id ? 'border-mnema-accent font-semibold text-mnema-text' : 'border-transparent text-mnema-muted hover:text-mnema-text']"
          @click="tab = id"
          @keydown="rovingKeydown($event, i, TABS.length, n => { tab = TABS[n]! })"
        >{{ $t(`webhooks.tab.${id}`) }}</button>
      </div>
      <div class="flex-1 overflow-y-auto p-5" role="tabpanel" :id="`webhooks-panel-${tab}`" :aria-labelledby="`webhooks-tab-${tab}`">
        <IncomingWebhooksPanel v-if="tab === 'incoming'" :channel-id="channel.id" />
        <OutgoingWebhooksPanel v-else :channel-id="channel.id" />
      </div>
    </div>
  </BaseDialog>
</template>
