<script setup>
// Admin dashboard: categories and channels, their order (drag or arrows),
// renames and deletes. The order is edited locally and saved in one go.
import { ref, onMounted } from 'vue'
import { FolderTree, Trash2, Edit2, ArrowUp, ArrowDown, Plus, Save, Hash, Volume2, GripVertical } from '@lucide/vue'
import { api } from '../lib/api'
import { confirm } from '../lib/confirm'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const chatStore = useChatStore()
const toasts = useToastStore()

const layoutCategories = ref([])
const layoutUncategorized = ref([])
const hasLayoutChanges = ref(false)
const isSavingLayout = ref(false)
const editingCategory = ref(null)
const editCategoryName = ref('')
const editingChannel = ref(null)
const editChannelName = ref('')
const editChannelTopic = ref('')
const newCategoryName = ref('')
const draggedCategoryIndex = ref(null)
const draggedChannelInfo = ref(null)

function showError(e) {
  toasts.error(e?.message || t('admin.unknownError'))
}

// ---- Loading and keeping unsaved order ----

// `patch` applies one server-side change to the local layout. It runs instead
// of the reload whenever an unsaved reorder exists (also one started while
// the reload was in flight), so that reorder is never thrown away.
async function loadChannelsData(patch) {
  try {
    const data = await api('/api/channels')
    if (hasLayoutChanges.value) {
      patch?.()
      return
    }
    layoutCategories.value = (data?.categories || []).map(cat => ({
      ...cat,
      channels: (cat.channels || []).map(ch => ({ ...ch }))
    }))
    layoutUncategorized.value = (data?.uncategorized || []).map(ch => ({ ...ch }))
    hasLayoutChanges.value = false
  } catch (e) {
    patch?.()
    showError(e)
  }
}

async function syncAfterEdit(patch) {
  await chatStore.fetchChannels()
  if (hasLayoutChanges.value) patch()
  else await loadChannelsData(patch)
}

function allChannelLists() {
  return [layoutUncategorized.value, ...layoutCategories.value.map(c => c.channels || (c.channels = []))]
}

function findChannel(id) {
  for (const list of allChannelLists()) {
    const ch = list.find(c => c.id === id)
    if (ch) return ch
  }
  return null
}

function removeChannelLocally(id) {
  for (const list of allChannelLists()) {
    const i = list.findIndex(c => c.id === id)
    if (i !== -1) list.splice(i, 1)
  }
}

// Channels of a deleted category become uncategorized (ON DELETE SET NULL).
function removeCategoryLocally(id) {
  const i = layoutCategories.value.findIndex(c => c.id === id)
  if (i === -1) return
  const [cat] = layoutCategories.value.splice(i, 1)
  for (const ch of cat.channels || []) {
    ch.category_id = null
    layoutUncategorized.value.push(ch)
  }
}

// ---- Ordering ----

function moveCategory(index, direction) {
  const targetIndex = index + direction
  if (targetIndex < 0 || targetIndex >= layoutCategories.value.length) return
  const item = layoutCategories.value.splice(index, 1)[0]
  layoutCategories.value.splice(targetIndex, 0, item)
  hasLayoutChanges.value = true
}

function moveChannel(categoryOrList, index, direction) {
  const list = Array.isArray(categoryOrList) ? categoryOrList : categoryOrList.channels
  const targetIndex = index + direction
  if (targetIndex < 0 || targetIndex >= list.length) return
  const item = list.splice(index, 1)[0]
  list.splice(targetIndex, 0, item)
  hasLayoutChanges.value = true
}

// Only one kind of drag is ever in progress; a drag cancelled with Escape or
// dropped outside ends in dragend, which forgets it.
function onDragEnd() {
  draggedCategoryIndex.value = null
  draggedChannelInfo.value = null
}

function onCategoryDragStart(e, index) {
  draggedChannelInfo.value = null
  draggedCategoryIndex.value = index
  if (e?.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
  }
}

function onCategoryDrop(e, targetIndex) {
  const sourceIndex = draggedCategoryIndex.value
  draggedCategoryIndex.value = null
  if (sourceIndex === null || sourceIndex === targetIndex) return
  const item = layoutCategories.value.splice(sourceIndex, 1)[0]
  layoutCategories.value.splice(targetIndex, 0, item)
  hasLayoutChanges.value = true
}

function onChannelDragStart(e, categoryId, index) {
  draggedCategoryIndex.value = null
  draggedChannelInfo.value = { categoryId, index }
  if (e?.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
  }
}

function onChannelDrop(e, targetCategoryId, targetIndex) {
  if (!draggedChannelInfo.value) return
  const { categoryId: sourceCatId, index: sourceIndex } = draggedChannelInfo.value

  let sourceList
  if (sourceCatId === null) {
    sourceList = layoutUncategorized.value
  } else {
    const cat = layoutCategories.value.find(c => c.id === sourceCatId)
    sourceList = cat ? cat.channels : null
  }

  let targetList
  if (targetCategoryId === null) {
    targetList = layoutUncategorized.value
  } else {
    const cat = layoutCategories.value.find(c => c.id === targetCategoryId)
    targetList = cat ? cat.channels : null
  }

  if (sourceList && targetList) {
    const [moved] = sourceList.splice(sourceIndex, 1)
    if (moved) {
      moved.category_id = targetCategoryId
      targetList.splice(targetIndex, 0, moved)
      hasLayoutChanges.value = true
    }
  }
  draggedChannelInfo.value = null
}

async function saveLayout() {
  isSavingLayout.value = true
  try {
    const categoriesPayload = layoutCategories.value.map((cat, idx) => ({
      id: cat.id,
      sort_order: idx
    }))
    const channelsPayload = []
    layoutUncategorized.value.forEach((ch, idx) => {
      channelsPayload.push({
        id: ch.id,
        category_id: null,
        sort_order: idx
      })
    })
    layoutCategories.value.forEach(cat => {
      ;(cat.channels || []).forEach((ch, idx) => {
        channelsPayload.push({
          id: ch.id,
          category_id: cat.id,
          sort_order: idx
        })
      })
    })

    await api('/api/admin/layout', {
      method: 'PUT',
      json: { categories: categoriesPayload, channels: channelsPayload }
    })
    hasLayoutChanges.value = false
    toasts.success(t('admin.layoutSaved'))
    await chatStore.fetchChannels()
    await loadChannelsData()
  } catch (e) {
    showError(e)
  } finally {
    isSavingLayout.value = false
  }
}

// ---- Editing ----

function openEditCategory(cat) {
  editingCategory.value = cat
  editCategoryName.value = cat.name || ''
}

function closeEditCategory() {
  editingCategory.value = null
  editCategoryName.value = ''
}

async function saveCategoryEdit() {
  if (!editingCategory.value || !editCategoryName.value.trim()) return
  const id = editingCategory.value.id
  const name = editCategoryName.value.trim()
  try {
    await api(`/api/admin/categories/${id}`, {
      method: 'PATCH',
      json: { name }
    })
    toasts.success(t('admin.categoryUpdated'))
    closeEditCategory()
    await syncAfterEdit(() => {
      const cat = layoutCategories.value.find(c => c.id === id)
      if (cat) cat.name = name
    })
  } catch (e) {
    showError(e)
  }
}

function openEditChannel(channel) {
  editingChannel.value = channel
  editChannelName.value = channel.name || ''
  editChannelTopic.value = channel.topic || ''
}

function closeEditChannel() {
  editingChannel.value = null
  editChannelName.value = ''
  editChannelTopic.value = ''
}

async function saveChannelEdit() {
  if (!editingChannel.value || !editChannelName.value.trim()) return
  const id = editingChannel.value.id
  const fields = {
    name: editChannelName.value.trim(),
    topic: editChannelTopic.value.trim()
  }
  try {
    const updated = await api(`/api/admin/channels/${id}`, {
      method: 'PATCH',
      json: fields
    })
    toasts.success(t('admin.channelUpdated'))
    closeEditChannel()
    await syncAfterEdit(() => {
      const ch = findChannel(id)
      // Name and topic only: category and position stay as reordered.
      if (ch) {
        ch.name = updated?.name ?? fields.name
        ch.topic = updated?.topic ?? fields.topic
      }
    })
  } catch (e) {
    showError(e)
  }
}

async function deleteCategory(cat) {
  const ok = await confirm({
    title: t('admin.deleteCategoryTitle', { name: cat.name }),
    body: t('admin.deleteCategoryBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/categories/${cat.id}`, { method: 'DELETE' })
    toasts.success(t('admin.categoryDeleted'))
    await syncAfterEdit(() => removeCategoryLocally(cat.id))
  } catch (e) {
    showError(e)
  }
}

async function deleteChannel(channel) {
  const ok = await confirm({
    title: t('admin.deleteChannelTitle', { name: channel.name }),
    body: t('admin.deleteChannelBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/channels/${channel.id}`, { method: 'DELETE' })
    toasts.success(t('admin.channelDeleted'))
    await syncAfterEdit(() => removeChannelLocally(channel.id))
  } catch (e) {
    showError(e)
  }
}

async function createCategory() {
  if (!newCategoryName.value.trim()) return
  try {
    const created = await api('/api/admin/categories', {
      method: 'POST',
      json: { name: newCategoryName.value.trim(), sort_order: layoutCategories.value.length }
    })
    newCategoryName.value = ''
    toasts.success(t('admin.categoryUpdated'))
    await syncAfterEdit(() => {
      if (created?.id && !layoutCategories.value.some(c => c.id === created.id)) {
        layoutCategories.value.push({ ...created, channels: [] })
      }
    })
  } catch (e) {
    showError(e)
  }
}

onMounted(() => loadChannelsData())
</script>

<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
          <FolderTree class="w-4 h-4 text-mnema-accent" />
          <span>{{ $t('admin.channelsLayout') }}</span>
        </h3>
      </div>

      <div class="flex flex-wrap items-center gap-2 ml-auto">
        <!-- Add Category form -->
        <div class="flex items-center gap-1.5">
          <input
            v-model="newCategoryName"
            type="text"
            :placeholder="$t('admin.categoryNamePlaceholder')"
            :aria-label="$t('admin.categoryNameLabel')"
            class="bg-mnema-surface border border-mnema-border-field rounded-md px-2.5 py-1 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition w-44"
            @keydown.enter="createCategory"
          />
          <button
            type="button"
            :disabled="!newCategoryName.trim()"
            class="bg-mnema-surface border border-mnema-border hover:border-mnema-accent text-mnema-text px-2.5 py-1 rounded-md text-sm transition disabled:opacity-40 flex items-center gap-1"
            @click="createCategory"
          >
            <Plus class="w-4 h-4" />
            <span>{{ $t('admin.addCategory') }}</span>
          </button>
        </div>

        <!-- Save Layout button -->
        <button
          data-testid="save-layout-button"
          type="button"
          :disabled="isSavingLayout"
          :class="[
            'flex items-center gap-1.5 px-3 py-1 rounded-md text-sm font-semibold transition shadow-sm',
            hasLayoutChanges
              ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover ring-2 ring-mnema-accent/40'
              : 'bg-mnema-elevated border border-mnema-border text-mnema-muted hover:text-mnema-text'
          ]"
          @click="saveLayout"
        >
          <Save class="w-4 h-4" />
          <span>{{ isSavingLayout ? $t('admin.save') : $t('admin.saveLayout') }}</span>
        </button>
      </div>
    </div>

    <!-- Unsaved banner notice -->
    <div v-if="hasLayoutChanges" class="p-3 rounded-lg bg-mnema-accent/10 border border-mnema-accent/30 text-xs text-mnema-mint flex items-center justify-between gap-3">
      <span>{{ $t('admin.hasUnsavedLayout') }}</span>
      <button
        type="button"
        class="bg-mnema-accent text-mnema-accent-ink px-3 py-1 rounded font-semibold hover:bg-mnema-accent-hover transition flex-shrink-0"
        @click="saveLayout"
      >
        {{ $t('admin.saveLayout') }}
      </button>
    </div>

    <!-- Uncategorized Channels -->
    <div v-if="layoutUncategorized.length" class="bg-mnema-surface rounded-lg border border-mnema-hairline p-3 space-y-2">
      <div class="flex items-center justify-between text-xs font-mono font-semibold uppercase text-mnema-tertiary">
        <span>{{ $t('admin.uncategorized') }}</span>
        <span>({{ layoutUncategorized.length }})</span>
      </div>

      <div class="space-y-1">
        <div
          v-for="(ch, chIdx) in layoutUncategorized"
          :key="ch.id"
          :data-testid="`channel-item-${ch.id}`"
          draggable="true"
          class="flex items-center justify-between gap-2 p-2 rounded-md bg-mnema-canvas border border-mnema-hairline hover:border-mnema-border transition-colors group cursor-grab active:cursor-grabbing"
          @dragstart="onChannelDragStart($event, null, chIdx)"
          @dragend="onDragEnd"
          @dragover.prevent
          @drop="onChannelDrop($event, null, chIdx)"
        >
          <div class="flex items-center gap-2 min-w-0 flex-1">
            <GripVertical class="w-4 h-4 text-mnema-tertiary group-hover:text-mnema-muted flex-shrink-0" />
            <Hash v-if="ch.type === 'text'" class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
            <Volume2 v-else class="w-4 h-4 text-mnema-accent flex-shrink-0" />
            <span class="text-sm font-medium text-mnema-text truncate">{{ ch.name }}</span>
            <span v-if="ch.topic" class="text-xs text-mnema-tertiary truncate">({{ ch.topic }})</span>
          </div>

          <div class="flex items-center gap-1 flex-shrink-0">
            <button
              data-testid="move-up-channel"
              type="button"
              :disabled="chIdx === 0"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-30"
              :title="$t('admin.moveUp')"
              :aria-label="$t('admin.moveUp')"
              @click="moveChannel(layoutUncategorized, chIdx, -1)"
            >
              <ArrowUp class="w-3.5 h-3.5" />
            </button>
            <button
              data-testid="move-down-channel"
              type="button"
              :disabled="chIdx === layoutUncategorized.length - 1"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-30"
              :title="$t('admin.moveDown')"
              :aria-label="$t('admin.moveDown')"
              @click="moveChannel(layoutUncategorized, chIdx, 1)"
            >
              <ArrowDown class="w-3.5 h-3.5" />
            </button>
            <button
              data-testid="rename-channel"
              type="button"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
              :title="$t('admin.renameChannel')"
              :aria-label="$t('admin.renameChannel')"
              @click="openEditChannel(ch)"
            >
              <Edit2 class="w-3.5 h-3.5" />
            </button>
            <button
              data-testid="delete-channel"
              type="button"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-surface transition"
              :title="$t('sidebar.deleteChannel')"
              :aria-label="$t('sidebar.deleteChannel')"
              @click="deleteChannel(ch)"
            >
              <Trash2 class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Categories & Channels Tree -->
    <div class="space-y-3">
      <div
        v-for="(cat, catIdx) in layoutCategories"
        :key="cat.id"
        :data-testid="`category-item-${cat.id}`"
        draggable="true"
        class="bg-mnema-surface rounded-lg border border-mnema-hairline p-3.5 space-y-2.5"
        @dragstart.self="onCategoryDragStart($event, catIdx)"
        @dragend="onDragEnd"
        @dragover.prevent
        @drop.self="onCategoryDrop($event, catIdx)"
      >
        <!-- Category Header -->
        <div class="flex items-center justify-between gap-2 border-b border-mnema-hairline/60 pb-2">
          <div class="flex items-center gap-2 min-w-0">
            <GripVertical class="w-4 h-4 text-mnema-tertiary cursor-grab active:cursor-grabbing flex-shrink-0" />
            <span class="text-xs font-semibold uppercase font-mono tracking-wider text-mnema-text truncate">{{ cat.name }}</span>
            <span class="text-xs text-mnema-tertiary font-mono">({{ cat.channels?.length || 0 }})</span>
          </div>

          <div class="flex items-center gap-1 flex-shrink-0">
            <button
              data-testid="move-up-category"
              type="button"
              :disabled="catIdx === 0"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition disabled:opacity-30"
              :title="$t('admin.moveUp')"
              :aria-label="$t('admin.moveUp')"
              @click="moveCategory(catIdx, -1)"
            >
              <ArrowUp class="w-3.5 h-3.5" />
            </button>
            <button
              data-testid="move-down-category"
              type="button"
              :disabled="catIdx === layoutCategories.length - 1"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition disabled:opacity-30"
              :title="$t('admin.moveDown')"
              :aria-label="$t('admin.moveDown')"
              @click="moveCategory(catIdx, 1)"
            >
              <ArrowDown class="w-3.5 h-3.5" />
            </button>
            <button
              data-testid="rename-category"
              type="button"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition"
              :title="$t('admin.renameCategory')"
              :aria-label="$t('admin.renameCategory')"
              @click="openEditCategory(cat)"
            >
              <Edit2 class="w-3.5 h-3.5" />
            </button>
            <button
              data-testid="delete-category"
              type="button"
              class="p-1 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-elevated transition"
              :title="$t('sidebar.deleteCategory')"
              :aria-label="$t('sidebar.deleteCategory')"
              @click="deleteCategory(cat)"
            >
              <Trash2 class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <!-- Channels inside category -->
        <div
          class="space-y-1.5 pl-3 border-l-2 border-mnema-border/40 min-h-[32px]"
          @dragover.prevent
          @drop="onChannelDrop($event, cat.id, (cat.channels || []).length)"
        >
          <div v-if="!cat.channels?.length" class="text-xs text-mnema-tertiary italic py-1">
            {{ $t('sidebar.noChannels') }}
          </div>

          <div
            v-for="(ch, chIdx) in cat.channels"
            :key="ch.id"
            :data-testid="`channel-item-${ch.id}`"
            draggable="true"
            class="flex items-center justify-between gap-2 p-2 rounded-md bg-mnema-canvas border border-mnema-hairline hover:border-mnema-border transition-colors group cursor-grab active:cursor-grabbing"
            @dragstart.stop="onChannelDragStart($event, cat.id, chIdx)"
            @dragend.stop="onDragEnd"
            @dragover.prevent.stop
            @drop.stop="onChannelDrop($event, cat.id, chIdx)"
          >
            <div class="flex items-center gap-2 min-w-0 flex-1">
              <GripVertical class="w-4 h-4 text-mnema-tertiary group-hover:text-mnema-muted flex-shrink-0" />
              <Hash v-if="ch.type === 'text'" class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
              <Volume2 v-else class="w-4 h-4 text-mnema-accent flex-shrink-0" />
              <span class="text-sm font-medium text-mnema-text truncate">{{ ch.name }}</span>
              <span v-if="ch.topic" class="text-xs text-mnema-tertiary truncate">({{ ch.topic }})</span>
            </div>

            <div class="flex items-center gap-1 flex-shrink-0">
              <button
                data-testid="move-up-channel"
                type="button"
                :disabled="chIdx === 0"
                class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-30"
                :title="$t('admin.moveUp')"
                :aria-label="$t('admin.moveUp')"
                @click.stop="moveChannel(cat, chIdx, -1)"
              >
                <ArrowUp class="w-3.5 h-3.5" />
              </button>
              <button
                data-testid="move-down-channel"
                type="button"
                :disabled="chIdx === (cat.channels || []).length - 1"
                class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-30"
                :title="$t('admin.moveDown')"
                :aria-label="$t('admin.moveDown')"
                @click.stop="moveChannel(cat, chIdx, 1)"
              >
                <ArrowDown class="w-3.5 h-3.5" />
              </button>
              <button
                data-testid="rename-channel"
                type="button"
                class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
                :title="$t('admin.renameChannel')"
                :aria-label="$t('admin.renameChannel')"
                @click.stop="openEditChannel(ch)"
              >
                <Edit2 class="w-3.5 h-3.5" />
              </button>
              <button
                data-testid="delete-channel"
                type="button"
                class="p-1 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-surface transition"
                :title="$t('sidebar.deleteChannel')"
                :aria-label="$t('sidebar.deleteChannel')"
                @click.stop="deleteChannel(ch)"
              >
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Edit Category Modal -->
    <BaseDialog
      v-if="editingCategory"
      :title="$t('admin.editCategoryTitle', { name: editingCategory.name })"
      panel-class="max-w-md"
      @close="closeEditCategory"
    >
      <form @submit.prevent="saveCategoryEdit" class="p-6 space-y-4">
        <div>
          <label class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
            {{ $t('admin.categoryNameLabel') }}
          </label>
          <input
            v-model="editCategoryName"
            type="text"
            :placeholder="$t('admin.categoryNamePlaceholder')"
            :aria-label="$t('admin.categoryNameLabel')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text outline-none focus:border-mnema-accent transition"
          />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button
            type="button"
            class="px-3.5 py-1.5 rounded-md border border-mnema-border text-sm text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition"
            @click="closeEditCategory"
          >
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            :disabled="!editCategoryName.trim()"
            class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-4 py-1.5 rounded-md text-sm transition disabled:opacity-40"
          >
            {{ $t('admin.save') }}
          </button>
        </div>
      </form>
    </BaseDialog>

    <!-- Edit Channel Modal -->
    <BaseDialog
      v-if="editingChannel"
      :title="$t('admin.editChannelTitle', { name: editingChannel.name })"
      panel-class="max-w-md"
      @close="closeEditChannel"
    >
      <form @submit.prevent="saveChannelEdit" class="p-6 space-y-4">
        <div>
          <label class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
            {{ $t('admin.channelNameLabel') }}
          </label>
          <input
            v-model="editChannelName"
            type="text"
            :placeholder="$t('admin.channelNamePlaceholder')"
            :aria-label="$t('admin.channelNameLabel')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text outline-none focus:border-mnema-accent transition"
          />
        </div>
        <div>
          <label class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
            {{ $t('admin.channelTopicLabel') }}
          </label>
          <input
            v-model="editChannelTopic"
            type="text"
            :placeholder="$t('admin.channelTopicPlaceholder')"
            :aria-label="$t('admin.channelTopicLabel')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text outline-none focus:border-mnema-accent transition"
          />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button
            type="button"
            class="px-3.5 py-1.5 rounded-md border border-mnema-border text-sm text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition"
            @click="closeEditChannel"
          >
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            :disabled="!editChannelName.trim()"
            class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-4 py-1.5 rounded-md text-sm transition disabled:opacity-40"
          >
            {{ $t('admin.save') }}
          </button>
        </div>
      </form>
    </BaseDialog>
  </section>
</template>
