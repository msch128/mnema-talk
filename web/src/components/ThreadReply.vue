<script setup lang="ts">
import type { PropType } from 'vue'
import type { Message } from '../types/domain'
import { Pencil, Trash2, Smile, Reply } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import ReplyPreview from './ReplyPreview.vue'
import ReactionPalette from './ReactionPalette.vue'
import MessageAttachments from './MessageAttachments.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import BotBadge from './BotBadge.vue'
import MessageEditor from './MessageEditor.vue'
import ReactionBar from './ReactionBar.vue'
import { formatTime, PICKER_ANCHOR, type useMessageActions } from '../composables/useMessageActions'

// One reply of a thread: hover actions, the reply it answers, the text (or its
// editor), attachments and reactions. What can be done with it is the thread's `actions`.
const props = defineProps({
  reply: { type: Object as PropType<Message>, required: true },
  actions: { type: Object as PropType<ReturnType<typeof useMessageActions>>, required: true },
  canReact: { type: Boolean, default: false },
  highlighted: { type: Boolean, default: false }
})
const emit = defineEmits<{ reply: []; jump: []; keydown: [event: KeyboardEvent]; 'open-image': [src: string] }>()

const chatStore = useChatStore()
const authStore = useAuthStore()
const { pickerId, editingId, editText } = props.actions
</script>

<template>
  <div
    :data-reply-id="reply.id"
    tabindex="0"
    role="article"
    @keydown="emit('keydown', $event)"
    :class="[
      'relative hover:bg-mnema-surface/50 -mx-2 px-2 py-1.5 rounded-md transition-colors group focus:outline-none focus-visible:bg-mnema-surface/40',
      highlighted ? 'msg-flash' : '',
      reply.user_id !== authStore.user?.id && chatStore.messageMentionsMe(reply) ? 'msg-mentions-me' : ''
    ]"
  >
    <!-- Hover Quick Actions Bar -->
    <div
      :class="[
        'absolute right-2 -top-2.5 items-center gap-0.5 bg-mnema-elevated border border-mnema-border rounded-lg p-0.5 shadow-md z-20 before:absolute before:-inset-2 before:content-[\'\'] before:-z-10',
        pickerId === reply.id ? 'flex' : 'hidden group-hover:flex group-focus-within:flex'
      ]"
    >
      <div v-if="canReact" :class="['relative', PICKER_ANCHOR]">
        <button
          type="button"
          @click.stop="actions.togglePicker(reply.id)"
          :class="[
            'p-1 rounded transition',
            pickerId === reply.id
              ? 'bg-mnema-surface text-amber-400'
              : 'hover:bg-mnema-surface text-mnema-tertiary hover:text-amber-400'
          ]"
          v-tooltip="$t('chat.addReaction')"
        >
          <Smile class="w-3.5 h-3.5" />
        </button>
        <ReactionPalette
          v-if="pickerId === reply.id"
          align="right"
          @pick="actions.toggleReaction(reply.id, $event)"
          @close="actions.closePicker()"
        />
      </div>

      <button
        type="button"
        @click.stop="emit('reply')"
        class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
        v-tooltip="$t('chat.reply')"
      >
        <Reply class="w-3.5 h-3.5" />
      </button>

      <button
        v-if="actions.isOwn(reply)"
        type="button"
        @click.stop="actions.startEdit(reply)"
        class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-accent transition"
        v-tooltip="$t('thread.edit')"
      >
        <Pencil class="w-3.5 h-3.5" />
      </button>

      <button
        v-if="actions.canDelete(reply)"
        type="button"
        @click.stop="actions.deleteMessage(reply)"
        class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-red-400 transition"
        v-tooltip="$t('thread.delete')"
      >
        <Trash2 class="w-3.5 h-3.5" />
      </button>
    </div>

    <!-- "Replied to" reference line; connector ends at the 32px avatar -->
    <div v-if="reply.reply_to" class="pl-11">
      <ReplyPreview :reply="reply.reply_to" spine-class="left-[-29px] w-[25px]" @jump="emit('jump')" />
    </div>

    <div class="flex items-start gap-3">
      <UserAvatar
        :user="reply"
        size="sm"
        class="cursor-pointer hover:opacity-85 transition mt-0.5"
        @click="chatStore.openUserProfile(reply)"
      />

      <div class="flex-1 min-w-0">
        <div class="flex items-baseline gap-2 min-w-0">
          <button
            type="button"
            data-testid="author-name"
            @click="chatStore.openUserProfile(reply)"
            class="font-semibold text-message text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer truncate text-left focus-visible:text-mnema-accent"
          >
            {{ reply.display_name || reply.username }}
          </button>
          <BotBadge v-if="reply.is_bot" :webhook="reply.is_webhook === true" />
          <span class="text-xs text-mnema-tertiary flex-shrink-0 tabular-nums">{{ formatTime(reply.created_at) }}</span>
          <span v-if="reply.is_edited" class="text-xs text-mnema-tertiary italic">{{ $t('chat.edited') }}</span>
        </div>

        <!-- Inline Reply Editor -->
        <MessageEditor
          v-if="editingId === reply.id"
          v-model="editText"
          compact
          :saving="actions.isSavingEdit.value"
          :label="$t('thread.edit')"
          @save="actions.saveEdit(reply)"
          @cancel="actions.cancelEdit()"
        />

        <MarkdownContent v-else-if="reply.content" :content="reply.content" class="mt-0.5" />
        <MessageEmbeds v-if="editingId !== reply.id" :embeds="reply.embeds ?? []" />

        <!-- Reply Attachments -->
        <MessageAttachments :attachments="reply.attachments" variant="reply" @open-image="emit('open-image', $event)" />

        <!-- Reaction badges -->
        <ReactionBar
          :reactions="reply.reactions"
          :can-add="canReact"
          compact
          small-icon
          :picker-open="pickerId === `bottom-${reply.id}`"
          @toggle="actions.toggleReaction(reply.id, $event)"
          @toggle-picker="actions.togglePicker(`bottom-${reply.id}`)"
          @close-picker="actions.closePicker()"
        />
      </div>
    </div>
  </div>
</template>
