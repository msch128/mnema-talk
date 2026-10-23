<script setup lang="ts">
import type { PropType } from 'vue'
import type { Message } from '../types/domain'
import { computed, watch } from 'vue'
import { Forward, Hash } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useForwardQuotesStore } from '../stores/forwardQuotes'
import { previewText } from '../lib/replies'
import { locale } from '../i18n'
import UserAvatar from './UserAvatar.vue'

// The original under a forwarded message. The message carries a reference
// only; the quote is what the server returns for this reader, so someone who
// may not see the original gets a plain "not available" and nothing else.
const props = defineProps({
  msg: { type: Object as PropType<Message>, required: true }
})

const chatStore = useChatStore()
const quotes = useForwardQuotesStore()
const quote = computed(() => {
  void quotes.epoch
  return quotes.quoteOf(props.msg.id)
})
watch(() => [props.msg.id, quotes.epoch], () => { void quotes.ensure(props.msg.id) }, { immediate: true })

const text = computed(() => previewText(quote.value?.content ?? ''))
const target = computed(() => {
  const q = quote.value
  return q?.available && q.message_id && q.channel_id ? { id: q.message_id, channel_id: q.channel_id } : null
})
const when = computed(() => (quote.value?.created_at
  ? new Date(quote.value.created_at).toLocaleDateString(locale.value, { month: 'short', day: 'numeric', year: 'numeric' }) : ''))

function open() {
  const to = target.value
  if (to) chatStore.goToMessage(to as unknown as Message)
}
</script>

<template>
  <div data-testid="forwarded" class="mt-1 max-w-xl rounded-md border-l-2 border-mnema-border-strong bg-mnema-surface/60 py-1.5 pl-3 pr-2">
    <p class="mb-1 flex items-center gap-1.5 text-xs text-mnema-tertiary">
      <Forward class="h-3.5 w-3.5" />{{ $t('forward.label') }}
      <span v-if="quote?.channel_name" class="flex items-center gap-0.5"><Hash class="h-3 w-3" />{{ quote.channel_name }}</span>
    </p>
    <template v-if="quote?.available && quote.author">
      <div class="mb-0.5 flex items-center gap-2">
        <UserAvatar :user="{ username: quote.author.username, display_name: quote.author.display_name, avatar_url: quote.author.avatar_url }" size="xxs" />
        <span class="truncate text-sm font-medium text-mnema-muted">{{ quote.author.display_name || quote.author.username }}</span>
        <span class="text-xs text-mnema-tertiary">{{ when }}</span>
      </div>
      <button v-if="target" type="button" data-testid="forwarded-open" class="block w-full text-left" @click.stop="open">
        <span class="line-clamp-4 wrap-break-word text-sm text-mnema-muted">{{ text || $t('chat.attachment') }}</span>
        <span v-if="quote.is_edited" class="text-xs italic text-mnema-tertiary">{{ $t('chat.edited') }}</span>
      </button>
      <p v-else class="line-clamp-4 wrap-break-word text-sm text-mnema-muted">{{ text || $t('chat.attachment') }}</p>
    </template>
    <p v-else-if="quote?.deleted" data-testid="forwarded-gone" class="text-sm italic text-mnema-tertiary">{{ $t('forward.deleted') }}</p>
    <p v-else-if="quote" data-testid="forwarded-unavailable" class="text-sm italic text-mnema-tertiary">{{ $t('forward.unavailable') }}</p>
  </div>
</template>
