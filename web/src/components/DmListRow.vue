<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatDirectMessage } from '../types/rest'
// One conversation in the sidebar: face, name, the start of the latest
// message, when, and what is new. Hovering (or focusing) a row offers the
// star and the close button; the same actions are in its context menu, which
// the "more" button opens on a phone.
import { computed } from 'vue'
import { BellOff, MoreHorizontal, Star, X } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { dmClock, dmPreview } from '../lib/dmList'
import { dmTitle } from '../lib/dm'
import { locale, t } from '../i18n'
import DmAvatar from './DmAvatar.vue'
import Badge from './Badge.vue'

const props = defineProps({
  dm: { type: Object as PropType<ChatDirectMessage>, required: true },
  open: { type: Boolean, default: false },
  unread: { type: Number, default: 0 },
  mentions: { type: Number, default: 0 },
  muted: { type: Boolean, default: false },
  // Whether the new features (favourites, closing) are offered.
  enhanced: { type: Boolean, default: false },
  favourite: { type: Boolean, default: false },
  draggable: { type: Boolean, default: false }
})
const emit = defineEmits<{
  open: []
  menu: [event: MouseEvent | KeyboardEvent | PointerEvent]
  hide: []
  favourite: []
  move: [step: -1 | 1]
}>()

const authStore = useAuthStore()
const me = computed(() => authStore.user?.id)
const title = computed(() => dmTitle(props.dm, me.value))
const clock = computed(() => dmClock(props.dm.last_message_at, locale.value))

const line = computed(() => {
  const preview = dmPreview(props.dm, me.value)
  if (preview.kind === 'none') return ''
  const text = preview.kind === 'text' ? preview.text : t(preview.kind === 'poll' ? 'dm.previewPoll' : 'dm.previewAttachment')
  if (preview.sender === 'me') return t('dm.previewYou', { text })
  return preview.sender ? t('dm.previewFrom', { name: preview.sender, text }) : text
})

const label = computed(() => [title.value, line.value, props.unread > 0 ? t('dm.unreadCount', { n: props.unread }) : ''].filter(Boolean).join(', '))

function onKey(event: KeyboardEvent) {
  if (event.target !== event.currentTarget) return
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    emit('open')
  } else if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
    event.preventDefault()
    emit('menu', event)
  } else if (props.favourite && event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
    event.preventDefault()
    emit('move', event.key === 'ArrowUp' ? -1 : 1)
  }
}
</script>

<template>
  <div
    role="button"
    tabindex="0"
    :data-dm="dm.id"
    :aria-current="open ? 'true' : undefined"
    :aria-label="label"
    :draggable="draggable ? 'true' : undefined"
    :class="[
      'group relative mb-px flex min-h-[46px] w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md px-2 py-1 text-left transition-colors focus:outline-hidden focus-ring max-md:min-h-14 motion-reduce:transition-none',
      open
        ? 'bg-mnema-hover text-mnema-text shadow-[inset_2px_0_0_0_#2DA771]'
        : unread > 0 && !muted
          ? 'text-mnema-text hover:bg-mnema-hover/70'
          : 'text-mnema-muted hover:bg-mnema-hover/70 hover:text-mnema-text'
    ]"
    @click="emit('open')"
    @contextmenu.prevent="emit('menu', $event)"
    @keydown="onKey"
  >
    <DmAvatar :dm="dm" />
    <div class="min-w-0 flex-1">
      <div class="flex items-baseline gap-1.5">
        <span :class="['truncate text-nav', unread > 0 && !muted ? 'font-semibold' : open ? 'font-medium' : '']" data-testid="dm-title">{{ title }}</span>
        <span v-if="dm.group" class="shrink-0 text-xs text-mnema-tertiary" data-testid="dm-count" :aria-label="$t('dm.memberCount', { n: dm.members.length })">{{ dm.members.length }}</span>
        <BellOff v-if="muted" class="h-3 w-3 shrink-0 self-center text-mnema-tertiary" data-testid="dm-muted" aria-hidden="true" />
        <time
          v-if="clock"
          :datetime="dm.last_message_at ?? undefined"
          :class="['ml-auto shrink-0 text-xs text-mnema-tertiary', enhanced ? 'md:group-hover:invisible md:group-focus-within:invisible max-md:hidden' : '']"
          data-testid="dm-time"
        >{{ clock }}</time>
      </div>
      <div class="flex min-h-4 items-center gap-1.5">
        <span class="truncate text-xs text-mnema-tertiary" data-testid="dm-preview">{{ line }}</span>
        <Badge
          v-if="mentions > 0 && !open && !muted"
          variant="mention"
          class="ml-auto"
          data-testid="dm-mentions"
          :aria-label="$t('dm.mentionCount', { n: mentions })"
        >@{{ mentions }}</Badge>
        <Badge v-else-if="unread > 0 && !open" variant="unread" class="ml-auto" data-testid="dm-unread">{{ unread }}</Badge>
      </div>
    </div>

    <div v-if="enhanced" class="absolute right-1.5 top-1 flex items-center gap-0.5 max-md:hidden md:invisible md:group-hover:visible md:group-focus-within:visible">
      <button
        type="button"
        data-testid="dm-star"
        class="btn btn-icon h-6 w-6"
        :aria-pressed="favourite ? 'true' : 'false'"
        :aria-label="favourite ? $t('dm.unfavourite') : $t('dm.favourite')"
        v-tooltip="favourite ? $t('dm.unfavourite') : $t('dm.favourite')"
        @click.stop="emit('favourite')"
      >
        <Star :class="['h-3.5 w-3.5', favourite ? 'fill-current text-mnema-accent' : '']" />
      </button>
      <button
        v-if="!favourite"
        type="button"
        data-testid="dm-close"
        class="btn btn-icon h-6 w-6"
        :aria-label="$t('dm.close')"
        v-tooltip="$t('dm.close')"
        @click.stop="emit('hide')"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>
    <button
      v-if="enhanced"
      type="button"
      data-testid="dm-more-actions"
      class="btn btn-icon h-11 w-11 shrink-0 md:hidden"
      aria-haspopup="menu"
      :aria-label="$t('dm.actions')"
      @click.stop="emit('menu', $event)"
    >
      <MoreHorizontal class="h-5 w-5" />
    </button>
  </div>
</template>
