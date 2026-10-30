<script setup lang="ts">
import { computed } from 'vue'
import { Lock, MessageSquare, Pin, CircleCheck } from '@lucide/vue'
import type { Message } from '../types/domain'
import { relative } from '../lib/time'
import { locale } from '../i18n'
import { previewText } from '../lib/replies'
import { shownName } from '../lib/deletedUser'
import UserAvatar from './UserAvatar.vue'
import ForumTagChip from './ForumTagChip.vue'

const props = defineProps<{
  post: Message
  unread?: boolean
}>()

const emit = defineEmits<{
  select: [post: Message]
}>()

const authorName = computed(() => shownName(props.post))
const snippet = computed(() => previewText(props.post.content))
const replyCount = computed(() => props.post.reply_count ?? 0)
// The first picture of the first message, as a small preview on the right.
const thumbnail = computed(() => props.post.attachments?.find(a => !a.is_deleted && a.mime_type.startsWith('image/')))
const active = computed(() => relative(props.post.last_activity_at ?? props.post.created_at, locale.value))
const created = computed(() => relative(props.post.created_at, locale.value))
</script>

<template>
  <article
    data-testid="forum-post-card"
    :data-unread="unread ? 'true' : undefined"
    tabindex="0"
    role="button"
    :aria-label="(post.title || $t('forum.untitled')) + (unread ? ' (' + $t('forum.unread') + ')' : '')"
    class="group flex w-full cursor-pointer gap-3 rounded-xl border border-mnema-border/70 bg-mnema-elevated p-4 text-left transition hover:border-mnema-accent/40 hover:bg-mnema-surface focus:outline-hidden focus-visible:border-mnema-accent max-sm:p-3.5"
    @click="emit('select', post)"
    @keydown.enter.prevent="emit('select', post)"
    @keydown.space.prevent="emit('select', post)"
  >
    <div class="flex min-w-0 flex-1 flex-col gap-2">
      <div class="flex min-w-0 items-start gap-2">
        <span v-if="unread" data-testid="forum-post-unread" class="mt-2 h-2 w-2 shrink-0 rounded-full bg-mnema-accent" :title="$t('forum.unread')"></span>
        <h3
          :class="[
            'line-clamp-2 min-w-0 flex-1 text-base text-mnema-text transition group-hover:text-mnema-accent',
            unread ? 'font-bold' : 'font-semibold'
          ]"
        >
          {{ post.title || $t('forum.untitled') }}
        </h3>
        <div class="flex shrink-0 items-center gap-1.5 pt-0.5 text-xs">
          <span v-if="post.is_pinned" data-testid="forum-post-pinned" class="flex items-center text-mnema-accent" :title="$t('forum.pinnedBadge')">
            <Pin class="h-3.5 w-3.5" :aria-label="$t('forum.pinnedBadge')" />
          </span>
          <span v-if="post.is_resolved" data-testid="forum-post-solved" class="flex items-center gap-1 rounded-md bg-emerald-400/10 px-1.5 py-0.5 text-emerald-300">
            <CircleCheck class="h-3.5 w-3.5" />
            <span class="max-sm:sr-only">{{ $t('forum.solvedBadge') }}</span>
          </span>
          <span v-if="post.is_locked" class="flex items-center gap-1 rounded-md bg-amber-400/10 px-1.5 py-0.5 text-amber-400" :title="$t('forum.lockedBadge')">
            <Lock class="h-3.5 w-3.5" />
            <span class="max-sm:sr-only">{{ $t('forum.lockedBadge') }}</span>
          </span>
        </div>
      </div>

      <p v-if="snippet" class="line-clamp-2 break-words text-sm text-mnema-secondary">{{ snippet }}</p>

      <div v-if="post.tags?.length" class="flex flex-wrap items-center gap-1.5">
        <ForumTagChip v-for="tag in post.tags" :key="tag.id" data-testid="forum-post-tag" :tag="tag" />
      </div>

      <div class="mt-auto flex min-w-0 items-center gap-2 text-xs text-mnema-tertiary">
        <UserAvatar :user="{ username: post.username, display_name: post.display_name, avatar_url: post.avatar_url }" size="sm" class="shrink-0" />
        <span class="truncate font-medium text-mnema-secondary">{{ authorName }}</span>
        <span class="shrink-0">{{ created }}</span>
        <span class="ml-auto flex shrink-0 items-center gap-3">
          <span v-if="replyCount > 0 && active" class="hidden sm:inline">{{ $t('forum.lastActivity', { when: active }) }}</span>
          <span class="flex items-center gap-1" :title="$t('forum.replyCount', { count: replyCount })">
            <MessageSquare class="h-3.5 w-3.5" />
            <span>{{ replyCount }}</span>
          </span>
        </span>
      </div>
    </div>

    <img
      v-if="thumbnail"
      data-testid="forum-post-thumbnail"
      :src="thumbnail.url"
      alt=""
      loading="lazy"
      class="h-20 w-20 shrink-0 self-start rounded-lg border border-mnema-hairline object-cover max-sm:h-16 max-sm:w-16"
    />
  </article>
</template>
