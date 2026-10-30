<script setup lang="ts">
// Admin dashboard: categories and channels, their order (drag or arrows),
// renames and deletes. The order is a draft: it is edited locally and saved in
// one go with the shared unsaved-changes bar. Renames and deletes are actions.
import { computed, ref, onMounted } from 'vue'
import { FolderTree } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { Channel, Category, ChannelHierarchy } from '../types/domain'
import { decodeChannelHierarchy, decodeChannel, decodeCategory } from '../types/domain'
import { confirm } from '../lib/confirm'
import {
  moveChannel, moveCategory, locateChannel, removeChannel, removeCategory, addCategory, nextSortOrder, toLayoutPayload
} from '../lib/channelLayout'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import AdminChannelRow from './AdminChannelRow.vue'
import AdminCategoryHeader from './AdminCategoryHeader.vue'
import AdminLayoutToolbar from './AdminLayoutToolbar.vue'
import AdminEditDialogs from './AdminEditDialogs.vue'
import CreateChannelModal from './CreateChannelModal.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'
import { onSaveShortcut, useLeaveGuard } from '../composables/draftGuard'
import { useLayoutDrag } from '../composables/useLayoutDrag'

const chatStore = useChatStore()
const toasts = useToastStore()

// { uncategorized, categories[].channels } as in lib/channelLayout.
const layout = ref<ChannelHierarchy>({ uncategorized: [], categories: [] })
// The order as last saved or loaded; the draft is dirty only while it differs from it.
const orderOf = (l: ChannelHierarchy) => JSON.stringify(toLayoutPayload(l))
const savedOrder = ref(orderOf(layout.value))
const hasLayoutChanges = computed(() => orderOf(layout.value) !== savedOrder.value)
useLeaveGuard(hasLayoutChanges)
const isSavingLayout = ref(false)
const isCreatingCategory = ref(false)
const editingCategory = ref<Category | null>(null)
const editingChannel = ref<Channel | null>(null)
const newCategoryName = ref('')
// The create-channel dialog; categoryId preselects its category ('' lets the dialog pick).
const creatingChannel = ref<{ categoryId: string } | null>(null)

function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

// ---- Loading and keeping unsaved order ----

// `patch` applies one server-side change to the local layout. It runs instead
// of the reload whenever an unsaved reorder exists (also one started while
// the reload was in flight), so that reorder is never thrown away.
async function loadChannelsData(patch?: () => void, force = false) {
  try {
    const data = await api('/api/channels', { decode: decodeChannelHierarchy })
    if (hasLayoutChanges.value && !force) {
      patch?.()
      return
    }
    layout.value = {
      uncategorized: (data?.uncategorized || []).map(ch => ({ ...ch })),
      categories: (data?.categories || []).map(cat => ({
        ...cat,
        channels: (cat.channels || []).map(ch => ({ ...ch }))
      }))
    }
    savedOrder.value = orderOf(layout.value)
  } catch (e) {
    patch?.()
    showError(e)
  }
}

async function syncAfterEdit(patch: () => void) {
  await chatStore.fetchChannels()
  if (hasLayoutChanges.value) patch()
  else await loadChannelsData(patch)
}

// ---- Ordering (shared rules in lib/channelLayout) ----

// Takes a reordered tree; an unchanged one (same object) is no change.
function reorder(next: ChannelHierarchy) {
  if (next === layout.value) return
  layout.value = next
}

function moveCategoryBy(cat: Category, direction: number) {
  const index = layout.value.categories.indexOf(cat)
  reorder(moveCategory(layout.value, cat.id, index + direction))
}

// The arrows keep a channel inside its own list (they are disabled at the ends).
function moveChannelBy(categoryId: string | null, channel: Channel, direction: number) {
  const from = locateChannel(layout.value, channel.id)
  if (from) reorder(moveChannel(layout.value, channel.id, categoryId, from.index + direction))
}

const { onDragEnd, onCategoryDragStart, onCategoryDrop, onChannelDragStart, onChannelDrop } = useLayoutDrag(() => layout.value, reorder)

async function saveLayout() {
  if (isSavingLayout.value || !hasLayoutChanges.value) return
  isSavingLayout.value = true
  const sent = layout.value
  try {
    await api('/api/admin/layout', {
      method: 'PUT',
      json: toLayoutPayload(sent)
    })
    // The server holds what was sent. A reorder made while the request ran
    // differs from it, stays unsaved and the reload below must not throw it away.
    savedOrder.value = orderOf(sent)
    toasts.success(t('admin.layoutSaved'))
    await chatStore.fetchChannels()
    await loadChannelsData()
  } catch (e) {
    showError(e)
  } finally {
    isSavingLayout.value = false
  }
}

// Throws the unsaved order away and shows the saved one again.
function resetLayout() {
  void loadChannelsData(undefined, true)
}

// ---- Editing ----

function openEditCategory(cat: Category) {
  editingCategory.value = cat
}

function closeEdit() {
  editingCategory.value = null
  editingChannel.value = null
}

// A refused rename reaches the dialog (it shows it inline and keeps the draft);
// a reload that fails after the rename went through is only reported.
async function afterEdit(patch: () => void) {
  try {
    await syncAfterEdit(patch)
  } catch (e) {
    showError(e)
  }
}

async function saveCategoryEdit(name: string) {
  if (!editingCategory.value) return
  const id = editingCategory.value.id
  await api(`/api/admin/categories/${id}`, {
    method: 'PATCH',
    json: { name }
  })
  toasts.success(t('admin.categoryUpdated'))
  closeEdit()
  await afterEdit(() => {
    const cat = layout.value.categories.find(c => c.id === id)
    if (cat) cat.name = name
  })
}

function openEditChannel(channel: Channel) {
  editingChannel.value = channel
}

async function saveChannelEdit(fields: { name: string; topic: string }) {
  if (!editingChannel.value) return
  const id = editingChannel.value.id
  const updated = await api(`/api/admin/channels/${id}`, {
    method: 'PATCH',
    json: fields,
    decode: decodeChannel
  })
  toasts.success(t('admin.channelUpdated'))
  closeEdit()
  await afterEdit(() => {
    const ch = locateChannel(layout.value, id)?.channel
    // Name and topic only: category and position stay as reordered.
    if (ch) {
      ch.name = updated?.name ?? fields.name
      ch.topic = updated?.topic ?? fields.topic
    }
  })
}

function submitEdit(fields: { name: string; topic: string }) {
  return editingCategory.value ? saveCategoryEdit(fields.name) : saveChannelEdit(fields)
}

async function deleteCategory(cat: Category) {
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
    await syncAfterEdit(() => { layout.value = removeCategory(layout.value, cat.id) })
  } catch (e) {
    showError(e)
  }
}

async function deleteChannel(channel: Channel) {
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
    await syncAfterEdit(() => { layout.value = removeChannel(layout.value, channel.id) })
  } catch (e) {
    showError(e)
  }
}

// The new channel is already on the server; an unsaved reorder keeps its order and gets the row appended.
async function onChannelCreated(channel: Channel) {
  await syncAfterEdit(() => {
    const target = channel.category_id ? layout.value.categories.find(c => c.id === channel.category_id) : null
    if (target) target.channels = [...(target.channels ?? []), { ...channel }]
    else layout.value = { ...layout.value, uncategorized: [...layout.value.uncategorized, { ...channel }] }
  })
}

async function createCategory() {
  if (isCreatingCategory.value || !newCategoryName.value.trim()) return
  isCreatingCategory.value = true
  try {
    const created = await api('/api/admin/categories', {
      method: 'POST',
      json: { name: newCategoryName.value.trim(), sort_order: nextSortOrder(layout.value.categories) },
      decode: decodeCategory
    })
    newCategoryName.value = ''
    toasts.success(t('sidebar.categoryCreated'))
    await syncAfterEdit(() => {
      layout.value = addCategory(layout.value, created)
    })
  } catch (e) {
    showError(e)
  } finally {
    isCreatingCategory.value = false
  }
}

onMounted(() => loadChannelsData())
</script>

<template>
  <section class="space-y-4" @keydown="onSaveShortcut($event, saveLayout)">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
          <FolderTree class="w-4 h-4 text-mnema-accent" />
          <span>{{ $t('admin.channelsLayout') }}</span>
        </h3>
      </div>

      <AdminLayoutToolbar
        v-model:new-category-name="newCategoryName"
        :creating="isCreatingCategory"
        @create="createCategory"
        @add-channel="creatingChannel = { categoryId: '' }"
      />
    </div>

    <!-- Uncategorized Channels -->
    <div v-if="layout.uncategorized.length" class="bg-mnema-surface rounded-lg border border-mnema-hairline p-3 space-y-2">
      <div class="flex items-center justify-between text-xs font-mono font-semibold uppercase text-mnema-tertiary">
        <span>{{ $t('admin.uncategorized') }}</span>
        <span>({{ layout.uncategorized.length }})</span>
      </div>

      <div class="space-y-1">
        <AdminChannelRow
          v-for="(ch, chIdx) in layout.uncategorized"
          :key="ch.id"
          :channel="ch"
          :index="chIdx"
          :count="layout.uncategorized.length"
          @dragstart="onChannelDragStart($event, ch)"
          @dragend="onDragEnd"
          @drop="onChannelDrop($event, null, chIdx)"
          @move="moveChannelBy(null, ch, $event)"
          @rename="openEditChannel(ch)"
          @delete="deleteChannel(ch)"
        />
      </div>
    </div>

    <!-- Categories & Channels Tree -->
    <div class="space-y-3">
      <div
        v-for="(cat, catIdx) in layout.categories"
        :key="cat.id"
        :data-testid="`category-item-${cat.id}`"
        draggable="true"
        class="bg-mnema-surface rounded-lg border border-mnema-hairline p-3.5 space-y-2.5"
        @dragstart.self="onCategoryDragStart($event, cat)"
        @dragend="onDragEnd"
        @dragover.prevent
        @drop.self="onCategoryDrop($event, catIdx)"
      >
        <AdminCategoryHeader
          :category="cat"
          :index="catIdx"
          :count="layout.categories.length"
          @move="moveCategoryBy(cat, $event)"
          @add-channel="creatingChannel = { categoryId: cat.id }"
          @rename="openEditCategory(cat)"
          @delete="deleteCategory(cat)"
        />

        <!-- Channels inside category -->
        <div
          class="space-y-1.5 pl-3 border-l-2 border-mnema-border/40 min-h-[32px]"
          @dragover.prevent
          @drop="onChannelDrop($event, cat.id, (cat.channels || []).length)"
        >
          <div v-if="!cat.channels?.length" class="text-xs text-mnema-tertiary italic py-1">
            {{ $t('sidebar.noChannels') }}
          </div>

          <AdminChannelRow
            v-for="(ch, chIdx) in cat.channels"
            :key="ch.id"
            :channel="ch"
            :index="chIdx"
            :count="(cat.channels || []).length"
            @dragstart="onChannelDragStart($event, ch)"
            @dragend="onDragEnd"
            @drop="onChannelDrop($event, cat.id, chIdx)"
            @move="moveChannelBy(cat.id, ch, $event)"
            @rename="openEditChannel(ch)"
            @delete="deleteChannel(ch)"
          />
        </div>
      </div>
    </div>

    <CreateChannelModal
      v-if="creatingChannel"
      :initial-category-id="creatingChannel.categoryId"
      @close="creatingChannel = null"
      @created="onChannelCreated"
    />

    <AdminEditDialogs
      v-if="editingCategory || editingChannel"
      :category="editingCategory"
      :channel="editingChannel"
      :submit="submitEdit"
      @close="closeEdit"
    />

    <UnsavedChangesBar
      sticky
      save-testid="save-layout-button"
      :dirty="hasLayoutChanges"
      :can-save="hasLayoutChanges && !isSavingLayout"
      :saving="isSavingLayout"
      @save="saveLayout"
      @reset="resetLayout"
    />
  </section>
</template>
