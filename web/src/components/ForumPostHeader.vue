<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import {
  Pencil, Check, X, Lock, Pin, CircleCheck, CircleDashed, Trash2, Ellipsis, Tags, ChevronsDown, Loader2
} from '@lucide/vue'
import type { ForumTag } from '../types/domain'
import { useChatStore } from '../stores/chat'
import { fetchForumTags, fetchForumSettings } from '../lib/forumApi'
import type { ForumSettings } from '../lib/forumApi'
import { useForumPostActions } from '../composables/useForumPostActions'
import { useForumPostRead } from '../composables/useForumPostRead'
import { useButtonMenu } from '../composables/useButtonMenu'
import type { ContextMenuItem } from './menuTypes'
import { t } from '../i18n'
import ContextMenu from './ContextMenu.vue'
import ForumTagChip from './ForumTagChip.vue'
import ForumTagPicker from './ForumTagPicker.vue'

// The top of an open forum post: its title and tags (both editable by the author
// and by moderators), its state (solved, pinned, locked), the actions on the post
// and the way to the first unread reply.
const emit = defineEmits<{ jump: [replyId: string] }>()

const chatStore = useChatStore()
const actions = useForumPostActions()
const { post } = actions
const read = useForumPostRead()

const editingTitle = ref(false)
const titleDraft = ref('')
const titleInput = ref<HTMLInputElement | null>(null)
const savingTitle = ref(false)

const editingTags = ref(false)
const tagDraft = ref<string[]>([])
const savingTags = ref(false)
const forumTags = ref<ForumTag[]>([])
const settings = ref<ForumSettings>({ require_tag: false, guidelines: '' })
const tagError = ref('')

async function startTitle() {
  if (!post.value) return
  titleDraft.value = post.value.title ?? ''
  editingTitle.value = true
  await nextTick()
  titleInput.value?.focus()
  titleInput.value?.select()
}

async function saveTitle() {
  const title = titleDraft.value.trim()
  if (!title || savingTitle.value) return
  if (title === post.value?.title) {
    editingTitle.value = false
    return
  }
  savingTitle.value = true
  if (await actions.rename(title)) editingTitle.value = false
  savingTitle.value = false
}

async function loadForum() {
  const channelId = actions.channelId.value
  try {
    ;[forumTags.value, settings.value] = await Promise.all([fetchForumTags(channelId), fetchForumSettings(channelId)])
  } catch (err) {
    console.warn('Failed to load the forum tags:', err)
  }
}

async function startTags() {
  if (!post.value) return
  tagDraft.value = (post.value.tags ?? []).map(tag => tag.id)
  tagError.value = ''
  editingTags.value = true
  await loadForum()
}

const usable = computed(() => forumTags.value.filter(tag => actions.canModerate.value || !tag.moderated))
const needsTag = computed(() => settings.value.require_tag && usable.value.length > 0)
const tagsValid = computed(() => !needsTag.value || tagDraft.value.length > 0)

async function saveTags() {
  if (!tagsValid.value || savingTags.value) return
  savingTags.value = true
  if (await actions.retag(tagDraft.value)) editingTags.value = false
  savingTags.value = false
}

// Another post, another forum or a tag change while the editors are open: start over.
watch(() => post.value?.id, () => {
  editingTitle.value = false
  editingTags.value = false
})
watch(() => chatStore.forumTagsSignal, () => { if (editingTags.value) void loadForum() })

function menuItems(): ContextMenuItem[] {
  const p = post.value
  if (!p) return []
  const items: ContextMenuItem[] = []
  if (actions.canEdit.value) {
    items.push({ id: 'title', label: t('forum.editTitle'), icon: Pencil, action: () => { void startTitle() } })
    items.push({ id: 'tags', label: t('forum.editTags'), icon: Tags, action: () => { void startTags() } })
    items.push({
      id: 'solved', icon: p.is_resolved ? CircleDashed : CircleCheck,
      label: p.is_resolved ? t('forum.reopen') : t('forum.markSolved'),
      action: () => { void actions.setSolved(!p.is_resolved) }
    })
  }
  if (actions.canModerate.value) {
    items.push({ id: 'pin', label: p.is_pinned ? t('forum.unpin') : t('forum.pin'), icon: Pin, action: () => { void actions.setPinned(!p.is_pinned) } })
    items.push({ id: 'lock', label: p.is_locked ? t('forum.unlockPost') : t('forum.lockPost'), icon: Lock, action: () => { void actions.setLocked(!p.is_locked) } })
  }
  if (actions.canDelete.value) {
    items.push({ type: 'separator' })
    items.push({ id: 'delete', label: t('forum.deletePost'), icon: Trash2, danger: true, action: () => { void actions.remove() } })
  }
  return items
}

const { menu, button, onPointerDown, toggle } = useButtonMenu(menuItems)
const hasActions = computed(() => menuItems().length > 0)

function jump() {
  const id = read.firstUnreadId.value
  if (id) emit('jump', id)
  read.dismiss()
}
</script>

<template>
  <section v-if="post" class="shrink-0 space-y-2 border-b border-mnema-hairline bg-mnema-canvas px-4 py-3" data-testid="forum-post-header">
    <div class="flex items-start gap-2">
      <form v-if="editingTitle" class="flex min-w-0 flex-1 items-center gap-1" @submit.prevent="saveTitle" @keydown.esc.stop.prevent="editingTitle = false">
        <input
          ref="titleInput"
          v-model="titleDraft"
          data-testid="forum-title-input"
          type="text"
          v-max-chars="100"
          :aria-label="$t('forum.titleLabel')"
          class="min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-base font-semibold text-mnema-text focus:outline-hidden focus:border-mnema-accent"
        />
        <button type="submit" :disabled="!titleDraft.trim() || savingTitle" class="btn btn-icon h-8 w-8 text-emerald-400 max-md:h-10 max-md:w-10" :aria-label="$t('common.save')" data-testid="forum-title-save">
          <Loader2 v-if="savingTitle" class="h-4 w-4 animate-spin" />
          <Check v-else class="h-4 w-4" />
        </button>
        <button type="button" class="btn btn-icon h-8 w-8 max-md:h-10 max-md:w-10" :aria-label="$t('common.cancel')" @click="editingTitle = false">
          <X class="h-4 w-4" />
        </button>
      </form>
      <h2 v-else data-testid="forum-post-title" class="min-w-0 flex-1 break-words text-base font-bold leading-snug text-mnema-text">{{ post.title }}</h2>

      <button
        v-if="hasActions && !editingTitle"
        ref="button"
        type="button"
        data-testid="forum-post-menu"
        aria-haspopup="menu"
        :aria-expanded="menu.state.open ? 'true' : 'false'"
        :aria-label="$t('forum.postActions')"
        v-tooltip="$t('forum.postActions')"
        class="btn btn-icon -mt-0.5 h-8 w-8 shrink-0 max-md:h-10 max-md:w-10"
        @pointerdown="onPointerDown"
        @click="toggle"
      >
        <Ellipsis class="h-5 w-5" />
      </button>
    </div>

    <div v-if="post.is_resolved || post.is_pinned || post.is_locked" class="flex flex-wrap items-center gap-1.5 text-xs">
      <span v-if="post.is_resolved" data-testid="forum-post-solved" class="flex items-center gap-1 rounded-md bg-emerald-400/10 px-1.5 py-0.5 text-emerald-300">
        <CircleCheck class="h-3.5 w-3.5" />{{ $t('forum.solvedBadge') }}
      </span>
      <span v-if="post.is_pinned" class="flex items-center gap-1 rounded-md bg-mnema-accent/10 px-1.5 py-0.5 text-mnema-accent">
        <Pin class="h-3.5 w-3.5" />{{ $t('forum.pinnedBadge') }}
      </span>
      <span v-if="post.is_locked" class="flex items-center gap-1 rounded-md bg-amber-400/10 px-1.5 py-0.5 text-amber-400">
        <Lock class="h-3.5 w-3.5" />{{ $t('forum.lockedBadge') }}
      </span>
    </div>

    <div v-if="editingTags" class="space-y-2 rounded-lg border border-mnema-hairline bg-mnema-surface/50 p-2.5" data-testid="forum-tag-edit">
      <ForumTagPicker v-model="tagDraft" label-id="post-header-tags" :tags="forumTags" :can-moderate="actions.canModerate.value" :required="needsTag" />
      <p v-if="forumTags.length === 0" class="text-xs text-mnema-tertiary">{{ $t('forum.noTags') }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs" @click="editingTags = false">{{ $t('common.cancel') }}</button>
        <button type="button" data-testid="forum-tags-save" :disabled="!tagsValid || savingTags" class="btn btn-primary flex items-center gap-1.5 px-3 py-1.5 text-xs" @click="saveTags">
          <Loader2 v-if="savingTags" class="h-3.5 w-3.5 animate-spin" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </div>
    </div>
    <div v-else-if="post.tags?.length || actions.canEdit.value" class="flex flex-wrap items-center gap-1.5">
      <ForumTagChip v-for="tag in post.tags" :key="tag.id" data-testid="forum-header-tag" :tag="tag" />
      <button
        v-if="actions.canEdit.value"
        type="button"
        data-testid="forum-edit-tags"
        class="inline-flex items-center gap-1 rounded-md border border-dashed border-mnema-border px-2 py-0.5 text-xs text-mnema-tertiary transition hover:border-mnema-accent hover:text-mnema-text max-md:py-1.5"
        @click="startTags"
      >
        <Tags class="h-3 w-3" />
        {{ post.tags?.length ? $t('forum.editTags') : $t('forum.addTags') }}
      </button>
    </div>

    <button
      v-if="read.unreadReplies.value.length"
      type="button"
      data-testid="forum-jump-unread"
      class="flex w-full items-center justify-between gap-2 rounded-lg border border-mnema-accent/30 bg-mnema-accent/10 px-3 py-1.5 text-xs font-medium text-mnema-text transition hover:bg-mnema-accent/15 max-md:py-2.5"
      @click="jump"
    >
      <span>{{ $t('forum.newReplies', { count: read.unreadReplies.value.length }) }}</span>
      <span class="flex items-center gap-1 text-mnema-accent">{{ $t('forum.jumpToUnread') }}<ChevronsDown class="h-3.5 w-3.5" /></span>
    </button>

    <ContextMenu
      v-model="menu.state.open"
      :x="menu.state.x"
      :y="menu.state.y"
      :anchor="menu.state.anchor"
      :items="menu.items.value"
      :min-width="200"
      :aria-label="$t('forum.postActions')"
    />
  </section>
</template>
