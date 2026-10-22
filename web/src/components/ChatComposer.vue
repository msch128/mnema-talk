<script setup lang="ts">
import type { PropType } from 'vue'
import type { Message } from '../types/domain'
import { ref } from 'vue'
import { Plus, ArrowUp, ChartNoAxesColumn } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import ReplyComposerBar from './ReplyComposerBar.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import SlashSuggestions from './SlashSuggestions.vue'
import SlashCommandBar from './SlashCommandBar.vue'
import PollDialog from './PollDialog.vue'
import { useDraftModel } from '../composables/useDraftModel'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useSlashCommands } from '../composables/useSlashCommands'
import { handleComposerKeydown } from '../composables/useComposerKeys'

// The message composer: text with @mention and slash-command suggestions,
// attachments, emoji and the reply bar. Sending and uploading are the chat's.
const props = defineProps({
  modelValue: { type: String, required: true },
  replyingTo: { type: Object as PropType<Message | null>, default: null },
  canSend: { type: Boolean, default: false },
  canAttach: { type: Boolean, default: false },
  placeholder: { type: String, default: '' },
  uploading: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  typingText: { type: String, default: '' }
})
const emit = defineEmits<{ 'update:modelValue': [text: string]; send: []; 'cancel-reply': []; file: [event: Event] }>()

const chatStore = useChatStore()
const text = useDraftModel(() => props.modelValue, value => emit('update:modelValue', value))

const fileInput = ref<HTMLInputElement | null>(null)
const textAreaEl = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(textAreaEl, text)
const pollOpen = ref(false)
const slash = useSlashCommands(text, () => chatStore.activeChannel?.id, () => { pollOpen.value = true })
defineExpose({ textArea: textAreaEl, focus: () => textAreaEl.value?.focus() })

function handleComposerInput() {
  if (text.value.trim()) chatStore.sendTyping(chatStore.activeChannel?.id)
}

function handleKeyDown(e: KeyboardEvent) {
  handleComposerKeydown(e, { consumers: [slash.onKeydown, assist.onKeydown], replying: () => !!props.replyingTo, cancelReply: () => emit('cancel-reply'), send: () => emit('send') })
}
</script>

<template>
  <div class="px-4 max-md:px-2 pb-1 flex-shrink-0">
    <SlashCommandBar
      v-if="slash.picked.value && canSend"
      :command="slash.picked.value"
      :values="slash.values"
      :running="slash.running.value"
      @set="(name, value) => (slash.values[name] = value)"
      @run="slash.run()"
      @cancel="slash.cancel(); textAreaEl?.focus()"
    />
    <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="emit('cancel-reply')" />
    <div
      :class="[
        'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 max-md:py-1 max-md:pr-1 flex items-center gap-1 sm:gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
        replyingTo ? 'rounded-b-lg' : 'rounded-lg'
      ]"
    >
      <SlashSuggestions
        v-if="slash.open.value && canSend"
        id="chat-slash"
        :items="slash.suggestions.value"
        :active="slash.active.value"
        @pick="slash.pick"
        @hover="slash.active.value = $event"
      />
      <MentionSuggestions
        v-if="assist.open.value"
        id="chat-mentions"
        :items="assist.suggestions.value"
        :active="assist.active.value"
        @pick="assist.pick"
        @hover="assist.active.value = $event"
      />

      <!-- Hidden File Input -->
      <input
        ref="fileInput"
        type="file"
        class="hidden"
        @change="emit('file', $event)"
        accept="image/*,video/*"
      />

      <!-- Attachment Button -->
      <button
        v-if="canAttach"
        @click="fileInput?.click()"
        :disabled="uploading || sending"
        class="w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center flex-shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
        v-tooltip="$t('chat.upload')"
      >
        <Plus class="w-5 h-5" />
      </button>

      <!-- Poll Button -->
      <button
        v-if="canSend"
        type="button"
        data-testid="poll-button"
        @click="pollOpen = true"
        :disabled="sending"
        class="w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center flex-shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
        v-tooltip="$t('poll.create')"
        :aria-label="$t('poll.create')"
      >
        <ChartNoAxesColumn class="w-5 h-5" />
      </button>

      <!-- Text Input -->
      <textarea
        ref="textAreaEl"
        v-model="text"
        @keydown="handleKeyDown"
        @input="handleComposerInput(); assist.onInput(); slash.onInput()"
        @click="assist.onInput"
        @keyup.left="assist.onInput"
        @keyup.right="assist.onInput"
        @blur="assist.close(); slash.close()"
        aria-autocomplete="list"
        :aria-expanded="assist.open.value ? 'true' : 'false'"
        :aria-controls="assist.open.value ? 'chat-mentions' : undefined"
        :aria-activedescendant="assist.open.value ? `chat-mentions-${assist.active.value}` : undefined"
        :placeholder="placeholder"
        :disabled="!canSend"
        :aria-label="placeholder"
        rows="1"
        class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
      ></textarea>

      <EmojiButton :disabled="sending || !canSend" @pick="assist.insertText" />

      <!-- Send Button -->
      <button
        @click="emit('send')"
        :disabled="!canSend || !text.trim() || uploading || sending"
        class="w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center flex-shrink-0 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
        v-tooltip="$t('chat.send')"
      >
        <ArrowUp class="w-4 h-4" />
      </button>
    </div>

    <PollDialog
      v-if="pollOpen && chatStore.activeChannel"
      :channel-id="chatStore.activeChannel.id"
      @close="pollOpen = false; textAreaEl?.focus()"
    />

    <!-- Who is typing (keeps its height so the layout doesn't jump) -->
    <div class="h-5 px-1 pt-0.5 text-xs text-mnema-tertiary truncate" role="status" aria-live="polite">
      {{ typingText }}
    </div>
  </div>
</template>
