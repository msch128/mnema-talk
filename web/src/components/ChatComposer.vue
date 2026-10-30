<script setup lang="ts">
import type { PropType } from 'vue'
import type { DraftReply } from '../lib/drafts'
import type { ContextMenuItem } from './menuTypes'
import { computed, ref } from 'vue'
import { Plus, ArrowUp, ChartNoAxesColumn, ChevronDown, CalendarClock, Paperclip } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { openLater } from '../lib/laterDialog'
import { t } from '../i18n'
import ContextMenu from './ContextMenu.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import EmojiButton from './EmojiButton.vue'
import MarkdownHelp from './MarkdownHelp.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import SlashSuggestions from './SlashSuggestions.vue'
import SlashCommandBar from './SlashCommandBar.vue'
import PollDialog from './PollDialog.vue'
import { useCompactLayout } from '../composables/useCompactLayout'
import { useDraftModel } from '../composables/useDraftModel'
import { useAutoGrow } from '../composables/useAutoGrow'
import { useComposerFiles } from '../composables/useComposerFiles'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useSlashCommands } from '../composables/useSlashCommands'
import { handleComposerKeydown } from '../composables/useComposerKeys'

// The message composer: text with @mention and slash-command suggestions,
// attachments, emoji and the reply bar. Sending and uploading are the chat's.
const props = defineProps({
  modelValue: { type: String, required: true },
  replyingTo: { type: Object as PropType<DraftReply | null>, default: null },
  canSend: { type: Boolean, default: false },
  canAttach: { type: Boolean, default: false },
  placeholder: { type: String, default: '' },
  uploading: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  typingText: { type: String, default: '' }
})
const emit = defineEmits<{ 'update:modelValue': [text: string]; send: []; 'cancel-reply': []; file: [event: Event]; 'arrow-up': [event: KeyboardEvent] }>()

const chatStore = useChatStore()
const features = useServerFeaturesStore()
const text = useDraftModel(() => props.modelValue, value => emit('update:modelValue', value))

const fileInput = ref<HTMLInputElement | null>(null)
const textAreaEl = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(textAreaEl, text)
const { multiline } = useAutoGrow(textAreaEl, text)
const { attachFiles, onPaste } = useComposerFiles({ fileInput, canAttach: () => props.canAttach, busy: () => props.uploading || props.sending })
const pollOpen = ref(false)
const slash = useSlashCommands(text, () => chatStore.activeChannel?.id, () => { pollOpen.value = true })
// "Send later" sits in a small menu next to the send button.
const laterButton = ref<HTMLElement | null>(null)
const laterMenuOpen = ref(false)
const laterEnabled = computed(() => features.has('messages.scheduled') && props.canSend)
const laterItem = computed<ContextMenuItem>(() => ({
  label: t('later.sendLater'),
  icon: CalendarClock,
  action: () => {
    const channel = chatStore.activeChannel
    if (!channel || !text.value.trim()) return
    openLater({
      kind: 'schedule', channelId: channel.id, content: text.value.trim(), replyToId: props.replyingTo?.id ?? null,
      onScheduled: () => { emit('update:modelValue', ''); emit('cancel-reply') }
    })
  }
}))
const laterItems = computed(() => [laterItem.value])

// On a phone the composer has room for the text: file, poll and "send later"
// share the one "+" button instead of three buttons of their own.
const { isCompact } = useCompactLayout()
const plusButton = ref<HTMLElement | null>(null)
const plusMenuOpen = ref(false)
const plusItems = computed<ContextMenuItem[]>(() => [
  ...(props.canAttach ? [{ label: t('chat.upload'), icon: Paperclip, disabled: props.uploading || props.sending, action: () => fileInput.value?.click() }] : []),
  ...(props.canSend ? [{ label: t('poll.create'), icon: ChartNoAxesColumn, disabled: props.sending, action: () => { pollOpen.value = true } }] : []),
  ...(laterEnabled.value ? [{ ...laterItem.value, disabled: !text.value.trim() || props.uploading || props.sending }] : [])
])
// One combobox, two lists: the textarea points at whichever is open.
const slashOpen = computed(() => slash.open.value && props.canSend)
const listboxId = computed(() => (slashOpen.value ? 'chat-slash' : assist.open.value ? 'chat-mentions' : undefined))
const activeOptionId = computed(() => (slashOpen.value ? `chat-slash-${slash.active.value}` : assist.open.value ? `chat-mentions-${assist.active.value}` : undefined))
const hasPlus = computed(() => (isCompact.value ? plusItems.value.length > 0 : props.canAttach))
function onPlus() {
  if (isCompact.value) plusMenuOpen.value = !plusMenuOpen.value
  else fileInput.value?.click()
}
defineExpose({ textArea: textAreaEl, attachFiles, focus: () => textAreaEl.value?.focus() })

function handleComposerInput() {
  if (text.value.trim()) chatStore.sendTyping(chatStore.activeChannel?.id)
}

function handleKeyDown(e: KeyboardEvent) {
  handleComposerKeydown(e, { consumers: [slash.onKeydown, assist.onKeydown], replying: () => !!props.replyingTo, cancelReply: () => emit('cancel-reply'), send: () => emit('send'), arrowUp: e => emit('arrow-up', e) })
}
</script>

<template>
  <div class="px-4 max-md:px-2 pb-1 shrink-0">
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
        'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 max-md:py-1 max-md:pr-1 flex gap-1 sm:gap-2 shadow-xs field-focus-within transition',
        multiline ? 'items-end' : 'items-center',
        replyingTo ? 'rounded-b-lg' : 'rounded-lg'
      ]"
    >
      <SlashSuggestions
        v-if="slashOpen"
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
        v-if="hasPlus"
        ref="plusButton"
        type="button"
        data-testid="composer-plus"
        @click="onPlus"
        :disabled="isCompact ? false : uploading || sending"
        :aria-haspopup="isCompact ? 'menu' : undefined"
        :aria-expanded="isCompact ? (plusMenuOpen ? 'true' : 'false') : undefined"
        class="focus-ring w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
        v-tooltip="$t('chat.upload')"
        :aria-label="$t('chat.upload')"
      >
        <Plus class="w-5 h-5" />
      </button>
      <ContextMenu v-if="isCompact" v-model="plusMenuOpen" :anchor="plusButton" :items="plusItems" :aria-label="$t('chat.upload')" />

      <!-- Poll Button -->
      <button
        v-if="canSend && !isCompact"
        type="button"
        data-testid="poll-button"
        @click="pollOpen = true"
        :disabled="sending"
        class="focus-ring w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
        v-tooltip="$t('poll.create')"
        :aria-label="$t('poll.create')"
      >
        <ChartNoAxesColumn class="w-5 h-5" />
      </button>

      <!-- Text Input -->
      <textarea
        ref="textAreaEl"
        data-bare
        data-composer-input
        v-model="text"
        @keydown="handleKeyDown"
        @paste="onPaste"
        @input="handleComposerInput(); assist.onInput(); slash.onInput()"
        @click="assist.onInput"
        @keyup.left="assist.onInput"
        @keyup.right="assist.onInput"
        @blur="assist.close(); slash.close()"
        aria-autocomplete="list"
        :aria-controls="listboxId"
        :aria-activedescendant="activeOptionId"
        :placeholder="placeholder"
        :disabled="!canSend"
        :aria-label="placeholder"
        rows="1"
        class="bg-transparent flex-1 min-w-0 resize-none outline-hidden text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
      ></textarea>

      <MarkdownHelp :disabled="!canSend" @insert="assist.insertText" />

      <EmojiButton :disabled="sending || !canSend" @pick="assist.insertText" />

      <!-- Send Button -->
      <button
        @click="emit('send')"
        :disabled="!canSend || !text.trim() || uploading || sending"
        class="btn btn-primary focus-ring w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center shrink-0 disabled:bg-mnema-surface disabled:text-mnema-tertiary p-0"
        v-tooltip="$t('chat.send')"
      >
        <ArrowUp class="w-4 h-4" />
      </button>

      <!-- Send later -->
      <template v-if="laterEnabled && !isCompact">
        <button
          ref="laterButton"
          type="button"
          data-testid="send-later-button"
          :disabled="!text.trim() || uploading || sending"
          class="focus-ring -ml-1 w-5 h-8 flex items-center justify-center shrink-0 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-50"
          aria-haspopup="menu"
          :aria-expanded="laterMenuOpen ? 'true' : 'false'"
          :aria-label="$t('later.sendOptions')"
          v-tooltip="$t('later.sendOptions')"
          @click="laterMenuOpen = !laterMenuOpen"
        >
          <ChevronDown class="w-4 h-4" />
        </button>
        <ContextMenu v-model="laterMenuOpen" :anchor="laterButton" :items="laterItems" :aria-label="$t('later.sendOptions')" />
      </template>
    </div>

    <PollDialog
      v-if="pollOpen && chatStore.activeChannel"
      :channel-id="chatStore.activeChannel.id"
      @close="pollOpen = false; textAreaEl?.focus()"
    />

    <!-- Who is typing (keeps its height so the layout doesn't jump) -->
    <div class="h-5 px-1 pt-0.5 text-xs text-mnema-tertiary truncate" role="status" aria-live="polite" data-testid="typing-indicator">
      {{ typingText }}
    </div>
  </div>
</template>
