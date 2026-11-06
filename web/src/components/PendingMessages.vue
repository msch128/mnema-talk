<script setup lang="ts">
// Text messages the member wrote while the connection was down. They show at
// the end of the conversation in muted text until they are sent; a refusal is
// the only thing that turns red. Each can be edited or discarded meanwhile.
import { computed, ref } from 'vue'
import { Clock, Loader2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'
import type { PendingMessage } from '../stores/chat/outboxTypes'
import MessageEditor from './MessageEditor.vue'

const props = defineProps<{ channelId: string; parentId?: string | null; compact?: boolean }>()
const chatStore = useChatStore()

const items = computed(() => chatStore.outbox.filter(p => p.channelId === props.channelId && p.parentId === (props.parentId ?? null)))
const editing = ref<string | null>(null)
const draft = ref('')

/** One announcement for the whole list: a status per row would read every message out on each change. */
const summary = computed(() => {
  const failed = items.value.filter(p => p.status === 'failed').length
  const waiting = items.value.length - failed
  const parts: string[] = []
  if (waiting) parts.push(t(waiting === 1 ? 'outbox.summaryWaiting' : 'outbox.summaryWaitingMany', { count: waiting }))
  if (failed) parts.push(t(failed === 1 ? 'outbox.summaryFailed' : 'outbox.summaryFailedMany', { count: failed }))
  return parts.join('. ')
})

/** A short piece of the text, so a button names the message it belongs to. */
function excerpt(text: string) {
  const line = text.replace(/\s+/g, ' ').trim()
  return line.length > 40 ? `${line.slice(0, 40)}…` : line
}

function waitingText(item: PendingMessage) {
  if (item.status === 'sending') return t('outbox.sending')
  return t(item.cause === 'server' ? 'outbox.retrying' : 'outbox.waiting')
}

function startEdit(nonce: string, content: string) {
  editing.value = nonce
  draft.value = content
}

function saveEdit(nonce: string) {
  chatStore.editPending(nonce, draft.value)
  editing.value = null
}
</script>

<template>
  <p v-if="items.length" class="sr-only" role="status" data-testid="pending-live">{{ summary }}</p>
  <div
    v-for="item in items"
    :key="item.nonce"
    data-testid="pending-message"
    :data-status="item.status"
    :class="compact ? 'py-1.5' : 'mx-2 mt-2 rounded-lg pl-16 pr-10 py-0.5 max-md:mx-0 max-md:rounded-none max-md:pl-[60px] max-md:pr-3'"
  >
    <MessageEditor
      v-if="editing === item.nonce"
      v-model="draft"
      :compact="compact"
      :label="$t('outbox.editLabel')"
      @save="saveEdit(item.nonce)"
      @cancel="editing = null"
    />
    <template v-else>
      <p class="text-message whitespace-pre-wrap break-words text-mnema-muted">{{ item.content }}</p>
      <div class="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <span v-if="item.status === 'failed'" class="text-mnema-danger">{{ $t('outbox.failed', { reason: item.error }) }}</span>
        <span v-else class="flex items-center gap-1 text-mnema-tertiary">
          <Loader2 v-if="item.status === 'sending'" class="w-3 h-3 animate-spin" />
          <Clock v-else class="w-3 h-3" />
          {{ waitingText(item) }}
        </span>
        <button v-if="item.status === 'failed'" type="button" class="text-mnema-muted hover:text-mnema-text underline-offset-2 hover:underline" :aria-label="$t('outbox.retryFor', { excerpt: excerpt(item.content) })" @click="chatStore.retryPending(item.nonce)">{{ $t('outbox.retry') }}</button>
        <template v-if="item.status !== 'sending'">
          <button v-if="!item.attempted" type="button" class="text-mnema-muted hover:text-mnema-text underline-offset-2 hover:underline" :aria-label="$t('outbox.editFor', { excerpt: excerpt(item.content) })" @click="startEdit(item.nonce, item.content)">{{ $t('outbox.edit') }}</button>
          <span v-else class="text-mnema-tertiary">{{ $t('outbox.noEdit') }}</span>
          <button type="button" class="text-mnema-muted hover:text-mnema-text underline-offset-2 hover:underline" :aria-label="$t('outbox.discardFor', { excerpt: excerpt(item.content) })" @click="chatStore.cancelPending(item.nonce)">{{ $t('outbox.cancel') }}</button>
        </template>
      </div>
    </template>
  </div>
</template>
