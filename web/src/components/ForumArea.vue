<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { MessagesSquare, Plus, Tags, Loader2, Search } from '@lucide/vue'
import type { ForumTag, Message } from '../types/domain'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { fetchForumTags, fetchForumSettings } from '../lib/forumApi'
import type { ForumSettings } from '../lib/forumApi'
import { useForumPosts } from '../composables/useForumPosts'
import { openSearch } from '../lib/globalSearch'
import { quoteValue } from '../lib/searchQuery'
import StatePanel from './StatePanel.vue'
import NavDrawerButton from './NavDrawerButton.vue'
import ForumPostCard from './ForumPostCard.vue'
import ForumToolbar from './ForumToolbar.vue'
import ForumEmptyState from './ForumEmptyState.vue'
import CreateForumPostModal from './CreateForumPostModal.vue'
import ManageForumTagsModal from './ManageForumTagsModal.vue'

const chatStore = useChatStore()
const permissions = usePermissionsStore()

const channelId = computed(() => chatStore.activeChannel?.id ?? '')
const channelName = computed(() => chatStore.activeChannel?.name ?? '')
const channelTopic = computed(() => chatStore.activeChannel?.topic ?? '')

const canPost = computed(() => permissions.canIn(channelId.value, 'send_messages'))
const canAttach = computed(() => permissions.canIn(channelId.value, 'attach_files'))
const canManageTags = computed(() => permissions.canIn(channelId.value, 'manage_channels'))
// Moderated tags are applied by those who may moderate messages.
const canModerate = computed(() => permissions.canIn(channelId.value, 'manage_messages'))

const tags = ref<ForumTag[]>([])
const settings = ref<ForumSettings>({ require_tag: false, guidelines: '' })
const list = useForumPosts(channelId, () => chatStore.activeThread?.id)
const { posts, unread, isLoading, isLoadingMore, hasMore, error, sort, tagId, search } = list

const showCreateModal = ref(false)
const showManageTagsModal = ref(false)

// Answers of a superseded request (older channel) are dropped.
let forumSeq = 0

async function loadForum() {
  const id = channelId.value
  if (!id) return
  const seq = ++forumSeq
  try {
    const [loadedTags, loadedSettings] = await Promise.all([fetchForumTags(id), fetchForumSettings(id)])
    if (seq !== forumSeq) return
    tags.value = loadedTags
    settings.value = loadedSettings
    // A filter on a tag that is gone shows nothing for good: let it go.
    if (tagId.value && !loadedTags.some(tag => tag.id === tagId.value)) tagId.value = null
  } catch (err) {
    if (seq === forumSeq) console.error('Failed to load forum tags:', err)
  }
}

function searchInForum() {
  if (channelName.value) void openSearch(`in:${quoteValue(channelName.value)} `)
}

function handleSelectPost(post: Message) {
  list.markRead(post.id)
  chatStore.openThread(post)
}

function handlePostCreated(newPost: Message) {
  list.add(newPost)
  chatStore.openThread(newPost)
}

function resetFilters() {
  search.value = ''
  tagId.value = null
}

// Opening a post by a link marks it read in the list as well.
watch(() => chatStore.activeThread?.id, id => { if (id) list.markRead(id) })
watch(channelId, () => { void loadForum() })
// Tags or rules changed (here or by another moderator): the posts carry the tags, so look at them again.
watch(() => chatStore.forumTagsSignal, signal => {
  if (signal?.channelId !== channelId.value) return
  void loadForum()
  void list.refresh()
})
onMounted(() => { void loadForum() })

const filtered = computed(() => !!tagId.value || !!search.value.trim())
</script>

<template>
  <main
    data-testid="forum-area"
    class="flex-1 flex flex-col h-full min-w-0 bg-mnema-canvas overflow-hidden"
  >
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
          class="btn btn-secondary px-3 py-1.5 text-xs flex items-center gap-1.5 max-sm:h-11 max-sm:w-11 max-sm:p-0 max-sm:justify-center"
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
          class="btn btn-primary px-3 py-1.5 text-xs flex items-center gap-1.5 max-sm:h-11 max-sm:w-11 max-sm:p-0 max-sm:justify-center"
          :aria-label="$t('forum.newPost')"
          @click="showCreateModal = true"
        >
          <Plus class="w-4 h-4" />
          <span class="max-sm:hidden">{{ $t('forum.newPost') }}</span>
        </button>
      </div>
    </header>

    <ForumToolbar v-model:search="search" v-model:sort="sort" v-model:tag-id="tagId" :tags="tags" />

    <div class="flex-1 overflow-y-auto p-4 max-sm:p-3 min-h-0">
      <div v-if="isLoading" class="h-64 flex flex-col items-center justify-center text-mnema-tertiary gap-2">
        <Loader2 class="w-6 h-6 animate-spin text-mnema-accent" />
        <span class="text-xs">{{ $t('forum.loadingPosts') }}</span>
      </div>

      <StatePanel v-else-if="error" state="error" retryable data-testid="forum-error" @retry="list.load">
        {{ error }}
      </StatePanel>

      <ForumEmptyState
        v-else-if="posts.length === 0"
        :filtered="filtered"
        :can-post="canPost"
        :guidelines="settings.guidelines"
        @create="showCreateModal = true"
        @reset="resetFilters"
      />

      <div v-else class="max-w-4xl mx-auto space-y-3 max-sm:space-y-2.5">
        <ForumPostCard
          v-for="post in posts"
          :key="post.id"
          :post="post"
          :unread="unread.has(post.id)"
          @select="handleSelectPost"
        />
        <div v-if="hasMore" class="flex justify-center pt-1">
          <button
            type="button"
            data-testid="forum-load-more"
            class="btn btn-secondary px-3 py-1.5 text-xs"
            :disabled="isLoadingMore"
            @click="list.loadMore"
          >
            {{ $t('common.loadMore') }}
          </button>
        </div>
      </div>
    </div>

    <CreateForumPostModal
      v-if="showCreateModal"
      :channel-id="channelId"
      :available-tags="tags"
      :settings="settings"
      :can-moderate="canModerate"
      :can-attach="canAttach"
      @close="showCreateModal = false"
      @created="handlePostCreated"
    />

    <ManageForumTagsModal
      v-if="showManageTagsModal"
      :channel-id="channelId"
      :tags="tags"
      :settings="settings"
      @close="showManageTagsModal = false"
      @updated="loadForum"
      @settings-saved="settings = $event"
    />
  </main>
</template>
