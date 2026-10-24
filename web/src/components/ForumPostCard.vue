<script setup lang="ts">
import { computed } from 'vue'
import { Lock, MessageSquare } from '@lucide/vue'
import type { Message } from '../types/domain'
import { clockTimeOptions } from '../lib/time'
import { locale } from '../i18n'
import { previewText } from '../lib/replies'
import { shownName } from '../lib/deletedUser'
import UserAvatar from './UserAvatar.vue'

const props = defineProps<{
  post: Message
}>()

const emit = defineEmits<{
  select: [post: Message]
}>()

const authorName = computed(() => shownName(props.post))
const snippet = computed(() => previewText(props.post.content))
const replyCount = computed(() => props.post.reply_count ?? 0)

function formatActivityDate(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  return d.toLocaleDateString(locale.value, {
    month: 'short',
    day: 'numeric',
    ...clockTimeOptions(locale.value)
  })
}
</script>

<template>
  <article
    data-testid="forum-post-card"
    tabindex="0"
    role="button"
    :aria-label="post.title || $t('forum.untitled')"
    class="w-full bg-mnema-elevated hover:bg-mnema-surface border border-mnema-border/70 hover:border-mnema-accent/40 rounded-xl p-4 cursor-pointer transition focus:outline-hidden focus-visible:border-mnema-accent flex flex-col gap-2.5 text-left group"
    @click="emit('select', post)"
    @keydown.enter.prevent="emit('select', post)"
    @keydown.space.prevent="emit('select', post)"
  >
    <!-- Header: Author info & Lock badge -->
    <div class="flex items-center justify-between gap-2 min-w-0">
      <div class="flex items-center gap-2 min-w-0">
        <UserAvatar
          :user="{ username: post.username, display_name: post.display_name, avatar_url: post.avatar_url }"
          size="sm"
          class="shrink-0"
        />
        <span class="text-sm font-medium text-mnema-text truncate">{{ authorName }}</span>
        <span class="text-xs text-mnema-tertiary shrink-0">
          {{ formatActivityDate(post.created_at) }}
        </span>
      </div>

      <div v-if="post.is_locked" class="flex items-center gap-1 text-xs text-amber-400 bg-amber-400/10 px-2 py-0.5 rounded-md shrink-0" :title="$t('forum.lockedBadge')">
        <Lock class="w-3.5 h-3.5" />
        <span>{{ $t('forum.lockedBadge') }}</span>
      </div>
    </div>

    <!-- Title -->
    <h3 class="text-base font-semibold text-mnema-text group-hover:text-mnema-accent transition line-clamp-2">
      {{ post.title || $t('forum.untitled') }}
    </h3>

    <!-- Content snippet -->
    <p v-if="snippet" class="text-sm text-mnema-secondary line-clamp-2 break-words">
      {{ snippet }}
    </p>

    <!-- Footer: Tags & Activity / Replies -->
    <div class="flex items-center justify-between gap-3 pt-1 border-t border-mnema-hairline/60 mt-1 min-w-0">
      <div class="flex items-center gap-1.5 flex-wrap min-w-0">
        <span
          v-for="tag in post.tags"
          :key="tag.id"
          data-testid="forum-post-tag"
          class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-medium bg-mnema-raised border border-mnema-hairline text-mnema-secondary"
        >
          <span v-if="tag.emoji">{{ tag.emoji }}</span>
          <span>{{ tag.name }}</span>
        </span>
      </div>

      <div class="flex items-center gap-3 shrink-0 text-xs text-mnema-tertiary ml-auto">
        <div class="flex items-center gap-1" :title="$t('forum.replyCount', { count: replyCount })">
          <MessageSquare class="w-3.5 h-3.5" />
          <span>{{ replyCount }}</span>
        </div>
        <span v-if="post.last_activity_at && post.last_activity_at !== post.created_at" class="hidden sm:inline">
          {{ $t('forum.activity') }}: {{ formatActivityDate(post.last_activity_at) }}
        </span>
      </div>
    </div>
  </article>
</template>
