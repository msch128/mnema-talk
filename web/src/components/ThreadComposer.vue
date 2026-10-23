<script setup lang="ts">
import type { PropType } from 'vue'
import type { DraftReply } from '../lib/drafts'
import { computed, ref } from 'vue'
import { Plus, ArrowUp } from '@lucide/vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import { useDraftModel } from '../composables/useDraftModel'
import { useComposerAssist } from '../composables/useComposerAssist'
import { handleComposerKeydown } from '../composables/useComposerKeys'

// The reply box of a thread: text with @mention suggestions, attachments,
// emoji and the reply bar. Sending and uploading are the thread's.
const props = defineProps({
  modelValue: { type: String, required: true },
  replyingTo: { type: Object as PropType<DraftReply | null>, default: null },
  canSend: { type: Boolean, default: false },
  canAttach: { type: Boolean, default: false },
  uploading: { type: Boolean, default: false },
  sending: { type: Boolean, default: false }
})
const emit = defineEmits<{ 'update:modelValue': [text: string]; send: []; 'cancel-reply': []; file: [event: Event] }>()

const text = useDraftModel(() => props.modelValue, value => emit('update:modelValue', value))

// Slash commands and polls start in the channel; here the text would go out as a plain reply.
const looksLikeCommand = computed(() => /^\/[A-Za-z]/.test(text.value))

const fileInput = ref<HTMLInputElement | null>(null)
const textAreaEl = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(textAreaEl, text)
defineExpose({ focus: () => textAreaEl.value?.focus() })

function handleKeyDown(e: KeyboardEvent) {
  handleComposerKeydown(e, { consumers: [assist.onKeydown], replying: () => !!props.replyingTo, cancelReply: () => emit('cancel-reply'), send: () => emit('send') })
}
</script>

<template>
  <div class="px-4 pb-6 pt-1 shrink-0">
    <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="emit('cancel-reply')" />
    <div
      :class="[
        'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-xs focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
        replyingTo ? 'rounded-b-lg' : 'rounded-lg'
      ]"
    >
      <MentionSuggestions
        v-if="assist.open.value"
        id="thread-mentions"
        :items="assist.suggestions.value"
        :active="assist.active.value"
        @pick="assist.pick"
        @hover="assist.active.value = $event"
      />

      <!-- Hidden file input for thread -->
      <input
        ref="fileInput"
        type="file"
        class="hidden"
        @change="emit('file', $event)"
        accept="image/*,video/*"
      />

      <button
        v-if="canAttach"
        @click="fileInput?.click()"
        :disabled="uploading || sending"
        class="w-8 h-8 flex items-center justify-center shrink-0 rounded-full text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-50"
        v-tooltip="$t('thread.upload')"
      >
        <Plus class="w-5 h-5" />
      </button>

      <textarea
        ref="textAreaEl"
        v-model="text"
        @keydown="handleKeyDown"
        @input="assist.onInput"
        @click="assist.onInput"
        @keyup.left="assist.onInput"
        @keyup.right="assist.onInput"
        @blur="assist.close"
        aria-autocomplete="list"
        :aria-expanded="assist.open.value ? 'true' : 'false'"
        :aria-controls="assist.open.value ? 'thread-mentions' : undefined"
        :aria-activedescendant="assist.open.value ? `thread-mentions-${assist.active.value}` : undefined"
        :placeholder="canSend ? $t('thread.placeholder') : $t('chat.readOnly')"
        :disabled="!canSend"
        :aria-label="canSend ? $t('thread.placeholder') : $t('chat.readOnly')"
        rows="1"
        class="bg-transparent flex-1 min-w-0 resize-none outline-hidden text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
      ></textarea>

      <EmojiButton :disabled="sending || !canSend" @pick="assist.insertText" />

      <button
        @click="emit('send')"
        :disabled="!canSend || !text.trim() || uploading || sending"
        class="btn btn-primary w-8 h-8 flex items-center justify-center shrink-0 disabled:bg-mnema-surface disabled:text-mnema-tertiary p-0"
        v-tooltip="$t('chat.send')"
      >
        <ArrowUp class="w-4 h-4" />
      </button>
    </div>
    <p v-if="looksLikeCommand" data-testid="thread-command-hint" role="status" class="mt-1 px-1 text-xs text-mnema-tertiary">{{ $t('thread.commandHint') }}</p>
  </div>
</template>
