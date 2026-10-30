<script setup lang="ts">
import type { PropType } from 'vue'
import { Reply, Pin, Pencil } from '@lucide/vue'
import type { Message } from '../types/domain'
import type { ThreadRoot } from '../stores/chat'
import { useChatStore } from '../stores/chat'
import { dateTime } from '../lib/time'
import { isRemovedMessage, shownName } from '../lib/deletedUser'
import { locale } from '../i18n'
import type { useMessageActions } from '../composables/useMessageActions'
import UserAvatar from './UserAvatar.vue'
import BotBadge from './BotBadge.vue'
import MarkdownContent from './MarkdownContent.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import PollCard from './PollCard.vue'
import MessageAttachments from './MessageAttachments.vue'
import MessageEditor from './MessageEditor.vue'
import ReactionBar from './ReactionBar.vue'

const props = defineProps({
  root: { type: Object as PropType<ThreadRoot>, required: true },
  actions: { type: Object as PropType<ReturnType<typeof useMessageActions>>, required: true },
  canReact: { type: Boolean, default: false },
  highlighted: { type: Boolean, default: false }
})

const emit = defineEmits<{
  reply: [root: ThreadRoot]
  keydown: [event: KeyboardEvent, root: ThreadRoot]
}>()

const chatStore = useChatStore()
const { pickerId, editingId, editText } = props.actions

const formatDate = (dateStr?: string) => dateTime(dateStr, locale.value)
</script>

<template>
  <div
    :data-reply-id="root.id"
    tabindex="0"
    role="article"
    @keydown="emit('keydown', $event, root)"
    :class="[
      'relative group bg-mnema-elevated border border-mnema-border/80 rounded-xl p-3.5 shadow-xs stack-2 focus:outline-hidden focus-ring',
      highlighted ? 'msg-flash' : ''
    ]"
  >
    <!-- Reply to the thread's root message, and edit it when it is mine -->
    <div class="absolute right-2 top-2 hidden items-center gap-1 group-hover:flex group-focus-within:flex">
      <button
        v-if="actions.isOwn(root as Message) && !root.poll && editingId !== root.id"
        type="button"
        data-testid="root-edit"
        @click.stop="actions.startEdit(root as Message)"
        class="btn btn-secondary btn-sm p-1"
        v-tooltip="$t('thread.edit')"
        :aria-label="$t('thread.edit')"
      >
        <Pencil class="w-3.5 h-3.5" />
      </button>
      <button
        type="button"
        @click.stop="emit('reply', root)"
        class="btn btn-secondary btn-sm p-1"
        v-tooltip="$t('chat.reply')"
      >
        <Reply class="w-3.5 h-3.5" />
      </button>
    </div>

    <!-- Author & timestamp -->
    <div class="flex items-center gap-2.5">
      <UserAvatar
        :user="root"
        size="md"
        class="cursor-pointer hover:opacity-85 transition"
        @click="chatStore.openUserProfile(root)"
      />
      <div class="min-w-0">
        <button
          type="button"
          data-testid="author-name"
          @click="chatStore.openUserProfile(root)"
          class="font-semibold text-message text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer truncate block max-w-full text-left focus-visible:text-mnema-accent"
        >
          {{ shownName(root) }}
        </button>
        <BotBadge v-if="root.is_bot" :webhook="root.is_webhook === true" />
        <span class="text-xs text-mnema-tertiary font-mono">
          {{ formatDate(root.created_at) }}
        </span>
      </div>
    </div>

    <MessageEditor
      v-if="editingId === root.id"
      v-model="editText"
      compact
      :saving="actions.isSavingEdit.value"
      :label="$t('thread.edit')"
      @save="actions.saveEdit(root as Message)"
      @cancel="actions.cancelEdit()"
    />
    <MarkdownContent v-else-if="root.content" :content="root.content" :mention-roles="root.mention_roles ?? []" />
    <p v-else-if="isRemovedMessage(root)" data-testid="message-removed" class="text-sm italic text-mnema-tertiary">{{ $t('chat.messageRemoved') }}</p>
    <MessageEmbeds :embeds="root.embeds ?? []" />
    <!-- A poll that started the thread keeps its options, votes and time left here -->
    <PollCard v-if="root.poll" :msg="root as Message" />
    <span v-if="root.is_edited" class="block text-xs text-mnema-tertiary italic">{{ $t('chat.edited') }}</span>
    <span v-if="root.is_pinned" data-testid="thread-root-pinned" class="flex items-center gap-1 text-xs text-mnema-accent">
      <Pin class="w-3.5 h-3.5" />{{ $t('moderation.pinned') }}
    </span>

    <!-- Root Message Attachments -->
    <MessageAttachments
      :attachments="root.attachments ?? []"
      variant="root"
      :message="root as Message"
    />
    <!-- Root message reactions -->
    <ReactionBar
      :reactions="root.reactions ?? []"
      :can-add="canReact"
      small-icon
      :picker-open="pickerId === `bottom-${root.id}`"
      @toggle="actions.toggleReaction(root.id, $event)"
      @toggle-picker="actions.togglePicker(`bottom-${root.id}`)"
      @close-picker="actions.closePicker()"
    />
  </div>
</template>
