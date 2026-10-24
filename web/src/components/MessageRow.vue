<script setup lang="ts">
import type { PropType } from 'vue'
import type { Message } from '../types/domain'
// One message in the channel timeline: hover/focus action bar, avatar or
// gutter time, header, inline editor or body, attachments, thread counter
// and reactions. Keyboard and context-menu handling stay with the list
// (bound on this component, they land on the row element).
import { ref, computed } from 'vue'
import { MessageSquare, Pencil, Smile, Reply, MoreHorizontal, Pin } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { isRemovedMessage, shownName } from '../lib/deletedUser'
import MarkdownContent from './MarkdownContent.vue'
import ReplyPreview from './ReplyPreview.vue'
import ForwardedQuote from './ForwardedQuote.vue'
import ReactionPalette from './ReactionPalette.vue'
import MessageAttachments from './MessageAttachments.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import PollCard from './PollCard.vue'
import BotBadge from './BotBadge.vue'
import MessageEditor from './MessageEditor.vue'
import ReactionBar from './ReactionBar.vue'
import { formatTime, PICKER_ANCHOR } from '../composables/useMessageActions'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { hasDraft, threadDraftKey } from '../lib/drafts'

const permissions = usePermissionsStore()
const authStore = useAuthStore()

const props = defineProps({
  msg: { type: Object as PropType<Message>, required: true },
  // Follow-up of the previous message by the same author: no header.
  grouped: { type: Boolean, default: false },
  highlighted: { type: Boolean, default: false },
  mentionsMe: { type: Boolean, default: false },
  isOwn: { type: Boolean, default: false },
  // May the member add reactions here (taking back their own always works).
  canReact: { type: Boolean, default: true },
  editing: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  // Id of the open reaction picker in the list (see useMessageActions).
  pickerId: { type: String as PropType<string | null>, default: null }
})
const editText = defineModel<string>('editText', { type: String, default: '' })
const emit = defineEmits<{ reply: []; edit: []; save: []; 'cancel-edit': []; more: [event: MouseEvent]; react: [emoji: string]; 'toggle-picker': [id: string]; 'close-picker': []; 'open-thread': []; 'open-profile': []; 'open-image': [src: string]; jump: [] }>()

const row = ref<HTMLElement | null>(null)
// An unsent reply in this message's thread, like the Draft marker of a channel.
const threadDraft = computed(() => hasDraft(authStore.user?.id, threadDraftKey(props.msg.id)))

defineExpose({ el: row })
</script>

<template>
  <div
    ref="row"
    :data-msg-id="msg.id"
    tabindex="0"
    role="article"
    :class="[
      'relative mx-2 rounded-lg pl-16 pr-10 max-md:mx-0 max-md:rounded-none max-md:pl-[60px] max-md:pr-3 py-0.5 hover:bg-mnema-surface/60 transition-colors group focus:outline-hidden focus-visible:bg-mnema-surface/40',
      grouped ? '' : 'mt-[17px] first:mt-2',
      highlighted ? 'msg-flash' : '',
      mentionsMe ? 'msg-mentions-me' : ''
    ]"
  >
    <!-- Hover Quick Actions Bar -->
    <div
      :class="[
        'absolute right-4 max-md:right-2 -top-4 items-center gap-0.5 bg-mnema-elevated border border-mnema-border rounded-lg p-1 shadow-lg z-20 before:absolute before:-inset-2 before:content-[\'\'] before:-z-10',
        pickerId === msg.id ? 'flex' : 'hidden group-hover:flex group-focus-within:flex'
      ]"
    >
      <!-- Emoji Reactions Trigger -->
      <div v-if="canReact" :class="['relative', PICKER_ANCHOR]">
        <button
          type="button"
          @click.stop="emit('toggle-picker', msg.id)"
          :class="[
            'p-1.5 rounded-sm transition max-md:flex max-md:h-11 max-md:w-11 max-md:items-center max-md:justify-center',
            pickerId === msg.id
              ? 'bg-mnema-surface text-amber-400'
              : 'hover:bg-mnema-surface text-mnema-tertiary hover:text-amber-400'
          ]"
          v-tooltip="$t('chat.addReaction')"
        >
          <Smile class="w-4 h-4" />
        </button>

        <ReactionPalette
          v-if="pickerId === msg.id"
          align="right"
          @pick="emit('react', $event)"
          @close="emit('close-picker')"
        />
      </div>

      <!-- Reply -->
      <button
        type="button"
        @click.stop="emit('reply')"
        class="btn btn-icon btn-sm max-md:flex max-md:h-11 max-md:w-11 max-md:items-center max-md:justify-center hover:text-mnema-text"
        v-tooltip="$t('chat.reply')"
      >
        <Reply class="w-4 h-4" />
      </button>

      <!-- Edit Message (if author) -->
      <button
        v-if="isOwn && !msg.poll"
        type="button"
        @click.stop="emit('edit')"
        class="btn btn-icon btn-sm max-md:flex max-md:h-11 max-md:w-11 max-md:items-center max-md:justify-center hover:text-mnema-accent"
        v-tooltip="$t('chat.edit')"
      >
        <Pencil class="w-4 h-4" />
      </button>

      <!-- More Actions (⋯) Context Menu Trigger -->
      <button
        type="button"
        @click.stop="emit('more', $event)"
        class="btn btn-icon btn-sm max-md:flex max-md:h-11 max-md:w-11 max-md:items-center max-md:justify-center hover:text-mnema-text"
        v-tooltip="$t('chat.moreActions')"
      >
        <MoreHorizontal class="w-4 h-4" />
      </button>
    </div>

    <!-- Grouped follow-up: small time in the gutter on hover -->
    <span
      v-if="grouped"
      class="absolute left-0 top-0.5 w-16 max-md:w-[72px] text-center text-xs leading-5.5 text-mnema-tertiary tabular-nums opacity-0 group-hover:opacity-100 select-none"
      aria-hidden="true"
    >
      {{ formatTime(msg.created_at) }}
    </span>

    <!-- User Avatar (first message of a group only) -->
    <UserAvatar
      v-else
      :user="msg"
      size="md"
      :class="['absolute! left-4 max-md:left-2 cursor-pointer hover:opacity-85 transition', msg.reply_to ? 'top-6' : 'top-1']"
      @click="emit('open-profile')"
    />

    <!-- Content Body -->
    <div class="min-w-0">
      <!-- "Replied to" reference line -->
      <ReplyPreview v-if="msg.reply_to" :reply="msg.reply_to" @jump="emit('jump')" />
      <div v-if="!grouped" class="flex items-baseline gap-2 min-w-0">
        <button
          type="button"
          data-testid="author-name"
          @click="emit('open-profile')"
          :style="permissions.nameColor(msg.user_id) ? { color: permissions.nameColor(msg.user_id) } : undefined"
          class="font-semibold text-message text-mnema-text hover:text-mnema-accent hover:underline transition-colors cursor-pointer truncate text-left focus-visible:underline focus-visible:text-mnema-accent"
        >
          {{ shownName(msg) }}
        </button>
        <BotBadge v-if="msg.is_bot" :webhook="msg.is_webhook === true" />
        <span class="text-xs text-mnema-tertiary shrink-0 tabular-nums">{{ formatTime(msg.created_at) }}</span>
        <span v-if="msg.is_edited" class="text-xs text-mnema-tertiary italic shrink-0">{{ $t('chat.edited') }}</span>
        <Pin v-if="msg.is_pinned" data-testid="pinned" class="w-3.5 h-3.5 shrink-0 text-mnema-accent" :aria-label="$t('moderation.pinned')" role="img" />
      </div>

      <!-- Inline Message Editor -->
      <MessageEditor
        v-if="editing"
        v-model="editText"
        :saving="saving"
        :label="$t('chat.edit')"
        @save="emit('save')"
        @cancel="emit('cancel-edit')"
      />

      <!-- Markdown Message Content -->
      <MarkdownContent v-else-if="msg.content" :content="msg.content" :mention-roles="msg.mention_roles ?? []" />
      <p v-else-if="isRemovedMessage(msg)" data-testid="message-removed" class="text-sm italic text-mnema-tertiary">{{ $t('chat.messageRemoved') }}</p>

      <!-- The original of a forwarded message, as this reader may see it -->
      <ForwardedQuote v-if="msg.forward" :msg="msg" />

      <!-- Webhook cards -->
      <MessageEmbeds :embeds="msg.embeds ?? []" :author="msg.display_name || msg.username" />

      <!-- Poll options and results -->
      <PollCard v-if="msg.poll" :msg="msg" />

      <!-- Grouped messages have no header line, so the edit marker follows the body -->
      <span
        v-if="grouped && msg.is_edited && !editing"
        class="block text-xs text-mnema-tertiary italic"
      >{{ $t('chat.edited') }}</span>
      <span v-if="grouped && msg.is_pinned && !editing" data-testid="pinned" class="flex items-center gap-1 text-xs text-mnema-accent">
        <Pin class="w-3.5 h-3.5" />{{ $t('moderation.pinned') }}
      </span>

      <!-- Media Attachments (Images, Clips, Documents) -->
      <MessageAttachments :attachments="msg.attachments" @open-image="emit('open-image', $event)" />

      <!-- Thread counter -->
      <div v-if="msg.reply_count > 0 || threadDraft" class="mt-2">
        <button
          type="button"
          @click.stop="emit('open-thread')"
          class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-mnema-accent-subtle/80 hover:bg-mnema-accent-subtle text-mnema-accent border border-mnema-accent/30 transition"
        >
          <MessageSquare class="w-4 h-4 text-mnema-accent" />
          <span v-if="msg.reply_count > 0">{{ $t('chat.replies', { count: msg.reply_count }) }}</span>
          <span class="text-xs opacity-75 font-mono ml-0.5">{{ $t('chat.openThread') }} &rarr;</span>
          <span v-if="threadDraft" class="shrink-0 rounded-sm bg-mnema-surface px-1 text-[10px] font-semibold uppercase tracking-wide text-mnema-tertiary" data-testid="thread-draft">{{ $t('sidebar.draft') }}</span>
        </button>
      </div>

      <!-- Reaction badges -->
      <ReactionBar
        :reactions="msg.reactions"
        :can-add="canReact"
        :picker-open="pickerId === `bottom-${msg.id}`"
        @toggle="emit('react', $event)"
        @toggle-picker="emit('toggle-picker', `bottom-${msg.id}`)"
        @close-picker="emit('close-picker')"
      />
    </div>
  </div>
</template>
