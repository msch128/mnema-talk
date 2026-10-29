<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { MessagesSquare, Plus, Tags, ArrowUpDown, Loader2, Search } from '@lucide/vue'
import type { ForumTag, Message } from '../types/domain'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { fetchForumTags, fetchForumPosts } from '../lib/forumApi'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import { openSearch } from '../lib/globalSearch'
import { quoteValue } from '../lib/searchQuery'
import StatePanel from './StatePanel.vue'
import NavDrawerButton from './NavDrawerButton.vue'
import ForumPostCard from './ForumPostCard.vue'
import CreateForumPostModal from './CreateForumPostModal.vue'
import ManageForumTagsModal from './ManageForumTagsModal.vue'

const chatStore = useChatStore()
const permissions = usePermissionsStore()
const toasts = useToastStore()

const channelId = computed(() => chatStore.activeChannel?.id ?? '')
const channelName = computed(() => chatStore.activeChannel?.name ?? '')
const channelTopic = computed(() => chatStore.activeChannel?.topic ?? '')

const canPost = computed(() => permissions.canIn(channelId.value, 'send_messages'))
const canManageTags = computed(() => permissions.canIn(channelId.value, 'manage_channels'))

const tags = ref<ForumTag[]>([])
const posts = ref<Message[]>([])
const isLoadingPosts = ref(false)
const isLoadingTags = ref(false)
const sortMode = ref<'activity' | 'newest'>('activity')
const selectedTagId = ref<string | null>(null)
const error = ref('')

const showCreateModal = ref(false)
const showManageTagsModal = ref(false)

// The server's default page size; a full page means there may be older posts.
const PAGE_SIZE = 50
const hasMore = ref(false)
const isLoadingMore = ref(false)
// Answers of a superseded request (older channel, sort or tag) are dropped.
let tagsSeq = 0
let postsSeq = 0

async function loadTags() {
  if (!channelId.value) return
  const seq = ++tagsSeq
  isLoadingTags.value = true
  try {
    const loaded = await fetchForumTags(channelId.value)
    if (seq === tagsSeq) tags.value = loaded
  } catch (err) {
    if (seq === tagsSeq) console.error('Failed to load forum tags:', err)
  } finally {
    if (seq === tagsSeq) isLoadingTags.value = false
  }
}

async function loadPosts() {
  if (!channelId.value) return
  const seq = ++postsSeq
  isLoadingPosts.value = true
  isLoadingMore.value = false
  error.value = ''
  try {
    const page = await fetchForumPosts(channelId.value, {
      sort: sortMode.value,
      tagId: selectedTagId.value ?? undefined
    })
    if (seq !== postsSeq) return
    posts.value = page
    hasMore.value = page.length >= PAGE_SIZE
  } catch (err) {
    if (seq !== postsSeq) return
    posts.value = []
    hasMore.value = false
    error.value = caughtErrorMessage(err, t('forum.loadFailed'))
  } finally {
    if (seq === postsSeq) isLoadingPosts.value = false
  }
}

async function loadMore() {
  const last = posts.value[posts.value.length - 1]
  if (!channelId.value || !last || isLoadingMore.value) return
  const seq = postsSeq
  isLoadingMore.value = true
  try {
    const page = await fetchForumPosts(channelId.value, {
      sort: sortMode.value,
      tagId: selectedTagId.value ?? undefined,
      before: last.id,
      beforeActivity: sortMode.value === 'activity' ? last.last_activity_at : undefined
    })
    if (seq !== postsSeq) return
    const known = new Set(posts.value.map(p => p.id))
    posts.value = [...posts.value, ...page.filter(p => !known.has(p.id))]
    hasMore.value = page.length >= PAGE_SIZE
  } catch (err) {
    if (seq !== postsSeq) return
    toasts.error(caughtErrorMessage(err, t('forum.loadFailed')))
  } finally {
    if (seq === postsSeq) isLoadingMore.value = false
  }
}

function searchInForum() {
  if (channelName.value) void openSearch(`in:${quoteValue(channelName.value)} `)
}

function handleSelectPost(post: Message) {
  chatStore.openThread(post)
}

function handlePostCreated(newPost: Message) {
  posts.value = [newPost, ...posts.value.filter(p => p.id !== newPost.id)]
  chatStore.openThread(newPost)
}

function selectTagFilter(tagId: string | null) {
  selectedTagId.value = selectedTagId.value === tagId ? null : tagId
}

watch(channelId, () => {
  loadTags()
  // Clearing a tag filter reloads through the filter watcher; load once.
  if (selectedTagId.value !== null) selectedTagId.value = null
  else loadPosts()
})

watch([sortMode, selectedTagId], () => {
  loadPosts()
})

watch(() => chatStore.forumTagsSignal, signal => {
  if (signal && signal.channelId === channelId.value) {
    loadTags()
  }
})

onMounted(() => {
  loadTags()
  loadPosts()
})
</script>

<template>
  <main
    data-testid="forum-area"
    class="flex-1 flex flex-col h-full min-w-0 bg-mnema-canvas overflow-hidden"
  >
    <!-- Header -->
    <header class="h-14 px-4 max-md:pl-1.5 max-md:pr-3 border-b border-mnema-hairline bg-mnema-raised flex items-center justify-between gap-3 shrink-0">
      <div class="flex items-center gap-2.5 max-md:gap-1.5 min-w-0">
        <NavDrawerButton />
        <MessagesSquare class="w-5 h-5 text-mnema-accent shrink-0" />
        <div class="min-w-0">
          <h1 class="text-base font-semibold text-mnema-text truncate">
            {{ channelName }}
          </h1>
          <p v-if="channelTopic" class="text-xs text-mnema-tertiary truncate">
            {{ channelTopic }}
          </p>
        </div>
      </div>

      <div class="flex items-center gap-2 shrink-0">
        <button
          type="button"
          data-testid="search-in-channel"
          class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
          v-tooltip="$t('chat.searchInChannel')"
          :aria-label="$t('chat.searchInChannel')"
          @click="searchInForum"
        >
          <Search class="w-5 h-5" />
        </button>
        <button
          v-if="canManageTags"
          type="button"
          data-testid="manage-tags-btn"
          class="btn btn-secondary px-3 py-1.5 text-xs flex items-center gap-1.5 max-sm:h-11 max-sm:w-11 max-sm:p-0"
          :aria-label="$t('forum.manageTags')"
          @click="showManageTagsModal = true"
        >
          <Tags class="w-3.5 h-3.5" />
          <span class="hidden sm:inline">{{ $t('forum.manageTags') }}</span>
        </button>

        <button
          v-if="canPost"
          type="button"
          data-testid="new-post-btn"
          class="btn btn-primary px-3 py-1.5 text-xs flex items-center gap-1.5 max-sm:h-11 max-sm:w-11 max-sm:p-0"
          :aria-label="$t('forum.newPost')"
          @click="showCreateModal = true"
        >
          <Plus class="w-4 h-4" />
          <span class="max-sm:hidden">{{ $t('forum.newPost') }}</span>
        </button>
      </div>
    </header>

    <!-- Toolbar: Filter by tag & Sort -->
    <section class="px-4 py-2.5 border-b border-mnema-hairline/80 bg-mnema-canvas flex items-center justify-between gap-3 shrink-0 flex-wrap">
      <!-- Tag Pills -->
      <div class="flex items-center gap-1.5 flex-wrap min-w-0">
        <button
          type="button"
          data-testid="filter-all-tags"
          :class="[
            'px-2.5 py-1 rounded-full text-xs font-medium transition cursor-pointer',
            selectedTagId === null
              ? 'bg-mnema-accent text-white shadow-xs'
              : 'bg-mnema-surface border border-mnema-hairline text-mnema-secondary hover:text-mnema-text hover:border-mnema-border'
          ]"
          @click="selectTagFilter(null)"
        >
          {{ $t('forum.allTags') }}
        </button>

        <button
          v-for="tag in tags"
          :key="tag.id"
          type="button"
          data-testid="filter-tag-pill"
          :class="[
            'inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium transition cursor-pointer',
            selectedTagId === tag.id
              ? 'bg-mnema-accent text-white shadow-xs'
              : 'bg-mnema-surface border border-mnema-hairline text-mnema-secondary hover:text-mnema-text hover:border-mnema-border'
          ]"
          @click="selectTagFilter(tag.id)"
        >
          <span v-if="tag.emoji">{{ tag.emoji }}</span>
          <span>{{ tag.name }}</span>
        </button>
      </div>

      <!-- Sort Toggle -->
      <div class="flex items-center gap-1.5 text-xs text-mnema-tertiary ml-auto shrink-0">
        <ArrowUpDown class="w-3.5 h-3.5" />
        <select
          v-model="sortMode"
          data-testid="forum-sort-select"
          class="bg-mnema-surface border border-mnema-border rounded-md px-2 py-1 text-xs text-mnema-text focus:outline-hidden cursor-pointer"
        >
          <option value="activity">{{ $t('forum.sortActivity') }}</option>
          <option value="newest">{{ $t('forum.sortNewest') }}</option>
        </select>
      </div>
    </section>

    <!-- Content: Posts List / Loading / Empty -->
    <div class="flex-1 overflow-y-auto p-4 min-h-0">
      <!-- Loading State -->
      <div v-if="isLoadingPosts" class="h-64 flex flex-col items-center justify-center text-mnema-tertiary gap-2">
        <Loader2 class="w-6 h-6 animate-spin text-mnema-accent" />
        <span class="text-xs">{{ $t('forum.loadingPosts') }}</span>
      </div>

      <StatePanel v-else-if="error" state="error" retryable data-testid="forum-error" @retry="loadPosts">
        {{ error }}
      </StatePanel>

      <!-- Empty State -->
      <div
        v-else-if="posts.length === 0"
        data-testid="forum-empty"
        class="h-64 flex flex-col items-center justify-center text-center p-6 gap-3"
      >
        <div class="w-12 h-12 rounded-full bg-mnema-raised border border-mnema-hairline flex items-center justify-center text-mnema-tertiary">
          <MessagesSquare class="w-6 h-6" />
        </div>
        <div class="space-y-1">
          <h2 class="text-sm font-semibold text-mnema-text">{{ $t('forum.emptyTitle') }}</h2>
          <p class="text-xs text-mnema-tertiary max-w-sm">
            {{ selectedTagId ? $t('forum.emptyTagFilter') : $t('forum.emptyDescription') }}
          </p>
        </div>
        <button
          v-if="canPost && !selectedTagId"
          type="button"
          class="btn btn-primary px-3.5 py-1.5 text-xs mt-1"
          @click="showCreateModal = true"
        >
          {{ $t('forum.newPost') }}
        </button>
      </div>

      <!-- Posts List -->
      <div v-else class="max-w-4xl mx-auto space-y-3">
        <ForumPostCard
          v-for="post in posts"
          :key="post.id"
          :post="post"
          @select="handleSelectPost"
        />
        <div v-if="hasMore" class="flex justify-center pt-1">
          <button
            type="button"
            data-testid="forum-load-more"
            class="btn btn-secondary px-3 py-1.5 text-xs"
            :disabled="isLoadingMore"
            @click="loadMore"
          >
            {{ $t('common.loadMore') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Modals -->
    <CreateForumPostModal
      v-if="showCreateModal"
      :channel-id="channelId"
      :available-tags="tags"
      @close="showCreateModal = false"
      @created="handlePostCreated"
    />

    <ManageForumTagsModal
      v-if="showManageTagsModal"
      :channel-id="channelId"
      :tags="tags"
      @close="showManageTagsModal = false"
      @updated="loadTags"
    />
  </main>
</template>
