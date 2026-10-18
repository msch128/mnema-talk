<script setup lang="ts">
import type { PropType } from 'vue'
import type { ThreadRoot } from '../stores/chat'
import { ref, watch } from 'vue'
import { Plus, ArrowUp } from '@lucide/vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import { useComposerAssist } from '../composables/useComposerAssist'
import { handleComposerKeydown } from '../composables/useComposerKeys'

// The reply box of a thread: text with @mention suggestions, attachments,
// emoji and the reply bar. Sending and uploading are the thread's.
const props = defineProps({
  modelValue: { type: String, required: true },
  replyingTo: { type: Object as PropType<ThreadRoot | null>, default: null },
  canSend: { type: Boolean, default: false },
  canAttach: { type: Boolean, default: false },
  uploading: { type: Boolean, default: false },
  sending: { type: Boolean, default: false }
})
const emit = defineEmits<{ 'update:modelValue': [text: string]; send: []; 'cancel-reply': []; file: [event: Event] }>()

// The draft: typing updates it at once (the suggestions read it in the same
// tick), the parent follows and may also set it.
const text = ref(props.modelValue)
watch(() => props.modelValue, value => { text.value = value })
watch(text, value => emit('update:modelValue', value), { flush: 'sync' })

const fileInput = ref<HTMLInputElement | null>(null)
const textAreaEl = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(textAreaEl, text)
defineExpose({ focus: () => textAreaEl.value?.focus() })

function handleKeyDown(e: KeyboardEvent) {
  handleComposerKeydown(e, { consumers: [assist.onKeydown], replying: () => !!props.replyingTo, cancelReply: () => emit('cancel-reply'), send: () => emit('send') })
}
</script>

<template>
  <div class="px-4 pb-6 pt-1 flex-shrink-0">
    <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="emit('cancel-reply')" />
    <div
      :class="[
        'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
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
        class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-full text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-50"
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
        class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
      ></textarea>

      <EmojiButton :disabled="sending || !canSend" @pick="assist.insertText" />

      <button
        @click="emit('send')"
        :disabled="!canSend || !text.trim() || uploading || sending"
        class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
        v-tooltip="$t('chat.send')"
      >
        <ArrowUp class="w-4 h-4" />
      </button>
    </div>
  </div>
</template>
