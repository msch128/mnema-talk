<script setup lang="ts">
// The "Direct messages" section at the top of the sidebar (and so of the
// phone drawer): favourites on top, then the conversations by latest
// activity, a box to find or start one, and the button that starts one. The
// whole section folds; a folded section keeps only its header, which shows
// what is new. Nothing shows on a server without direct messages.
import type { ChatDirectMessage } from '../types/rest'
import { computed, ref, watch } from 'vue'
import { ChevronDown, ChevronRight, Plus } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { useVoiceStore } from '../stores/voice'
import { useCompactLayout } from '../composables/useCompactLayout'
import { useDmActions } from '../composables/useDmActions'
import { useDmListActions } from '../composables/useDmListActions'
import { splitDms } from '../lib/dmList'
import { t } from '../i18n'
import DmCreateDialog from './DmCreateDialog.vue'
import DmFindBox from './DmFindBox.vue'
import DmListRow from './DmListRow.vue'
import Badge from './Badge.vue'
import ContextMenu from './ContextMenu.vue'
import type { ContextMenuItem } from './menuTypes'

const COLLAPSED_KEY = 'mnema_dm_collapsed'
// Longer lists show the newest conversations and a button for the rest.
const SHOWN = 8

const chatStore = useChatStore()
const authStore = useAuthStore()
const features = useServerFeaturesStore()
const toasts = useToastStore()
const voiceStore = useVoiceStore()
const { closeNav } = useCompactLayout()
const { canStart } = useDmActions()
const actions = useDmListActions()

const enhanced = computed(() => features.dmV2)

// The folded state is the member's, kept on the server so it follows them; the
// browser remembers it too, so the section is right before the server answers.
const storageKey = computed(() => `${COLLAPSED_KEY}:${authStore.user?.id ?? ''}`)
function readLocal() {
  try {
    return localStorage.getItem(storageKey.value) === '1'
  } catch {
    return false
  }
}
const local = ref(readLocal())
const collapsed = computed(() => (enhanced.value && chatStore.dmSettingsLoaded ? chatStore.dmSettings.list_collapsed : local.value))
watch(() => (chatStore.dmSettingsLoaded ? chatStore.dmSettings.list_collapsed : null), value => {
  if (value !== null) remember(value)
})

function remember(value: boolean) {
  local.value = value
  try {
    localStorage.setItem(storageKey.value, value ? '1' : '0')
  } catch {
    // Not remembered here; the server still has it.
  }
}

function toggle() {
  const next = !collapsed.value
  remember(next)
  if (!enhanced.value) return
  chatStore.saveDmSettings({ list_collapsed: next }).catch(() => toasts.error(t('dm.prefsFailed')))
}

const showAll = ref(false)
const creating = ref(false)

const visible = computed(() => features.dm && (chatStore.dms.length > 0 || canStart.value))
const unreadOf = (id: string) => chatStore.readStates[id]?.unread_count || 0
const mentionsOf = (id: string) => chatStore.readStates[id]?.mention_count || 0
const isMuted = (id: string) => chatStore.notificationLevel(id) === 'mute'
const isOpen = (id: string) => chatStore.activeChannel?.id === id && voiceStore.activeView === 'chat'
const unreadTotal = computed(() => chatStore.dms.reduce((sum, dm) => sum + unreadOf(dm.id), 0))

const lists = computed(() => {
  if (!enhanced.value) return { favourites: [], recent: chatStore.dms }
  return splitDms(chatStore.dms, chatStore.activeChannel?.id)
})
const recentShown = computed(() => (showAll.value ? lists.value.recent : lists.value.recent.slice(0, SHOWN)))
const hiddenCount = computed(() => (showAll.value ? 0 : Math.max(0, lists.value.recent.length - SHOWN)))
const isEmpty = computed(() => lists.value.favourites.length + lists.value.recent.length === 0)

function open(id: string) {
  closeNav()
  void chatStore.openDm(id)
}

// ---- Context menu ----
const menu = ref<{ open: boolean; x: number; y: number; items: ContextMenuItem[] }>({ open: false, x: 0, y: 0, items: [] })

function openMenu(dm: ChatDirectMessage, event: MouseEvent | KeyboardEvent) {
  const at = lists.value.favourites.findIndex(f => f.id === dm.id)
  const position = at < 0 ? null : { first: at === 0, last: at === lists.value.favourites.length - 1 }
  const target = event.currentTarget instanceof Element ? event.currentTarget.getBoundingClientRect() : null
  const pointer = event instanceof MouseEvent && (event.clientX > 0 || event.clientY > 0)
  menu.value = {
    open: true,
    x: pointer ? event.clientX : (target?.left ?? 0) + 24,
    y: pointer ? event.clientY : (target?.bottom ?? 0),
    items: actions.menuItems(dm, unreadOf(dm.id), position)
  }
}

// ---- Dragging a favourite to a new place ----
const dragging = ref<string | null>(null)
const dropAt = ref<{ id: string; after: boolean } | null>(null)

function onDragOver(event: DragEvent, id: string) {
  if (!dragging.value || dragging.value === id) return
  event.preventDefault()
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  dropAt.value = { id, after: event.clientY > rect.top + rect.height / 2 }
}

function onDrop(event: DragEvent) {
  event.preventDefault()
  const from = dragging.value
  const to = dropAt.value
  dragging.value = null
  dropAt.value = null
  if (!from || !to) return
  const ids = lists.value.favourites.map(dm => dm.id).filter(id => id !== from)
  const at = ids.indexOf(to.id) + (to.after ? 1 : 0)
  ids.splice(at, 0, from)
  void actions.reorder(ids)
}

function onDragEnd() {
  dragging.value = null
  dropAt.value = null
}
</script>

<template>
  <section v-if="visible" class="mb-3" data-testid="dm-section" :aria-label="$t('dm.section')">
    <div class="relative flex h-6 items-center gap-1 pr-1 max-md:h-11">
      <button
        type="button"
        class="flex h-6 min-w-0 flex-1 items-center gap-0.5 rounded-sm pl-0.5 text-left text-xs font-semibold uppercase tracking-wide text-mnema-tertiary transition-colors hover:text-mnema-muted focus:outline-hidden focus-visible:text-mnema-text focus-ring max-md:h-11"
        :aria-expanded="collapsed ? 'false' : 'true'"
        data-testid="dm-toggle"
        @click="toggle"
      >
        <ChevronRight v-if="collapsed" class="h-3 w-3 shrink-0" />
        <ChevronDown v-else class="h-3 w-3 shrink-0" />
        <span class="truncate">{{ $t('dm.section') }}</span>
        <Badge
          v-if="collapsed && unreadTotal > 0"
          variant="mention"
          class="ml-auto normal-case tracking-normal"
          :aria-label="$t('dm.unreadTotal', { n: unreadTotal })"
        >{{ unreadTotal }}</Badge>
      </button>
      <button
        v-if="canStart"
        type="button"
        data-testid="dm-new"
        class="btn btn-icon h-5 w-5 shrink-0 max-md:h-11 max-md:w-11"
        v-tooltip="$t('dm.new')"
        :aria-label="$t('dm.new')"
        @click="creating = true"
      >
        <Plus class="h-4 w-4" />
      </button>
    </div>

    <div v-if="!collapsed" class="mt-0.5" data-testid="dm-body">
      <DmFindBox v-if="enhanced && (canStart || chatStore.dms.length > 0)" @done="closeNav()" />

      <template v-if="lists.favourites.length">
        <p class="px-2 pb-0.5 pt-1 text-xs font-medium text-mnema-tertiary" data-testid="dm-favourites-label">{{ $t('dm.favourites') }}</p>
        <div data-testid="dm-favourites" @drop="onDrop">
          <DmListRow
            v-for="dm in lists.favourites"
            :key="dm.id"
            :dm="dm"
            enhanced
            favourite
            draggable
            :open="isOpen(dm.id)"
            :unread="unreadOf(dm.id)"
            :mentions="mentionsOf(dm.id)"
            :muted="isMuted(dm.id)"
            :class="[
              dragging === dm.id ? 'opacity-50' : '',
              dropAt?.id === dm.id ? (dropAt.after ? 'shadow-[inset_0_-2px_0_0_#2DA771]' : 'shadow-[inset_0_2px_0_0_#2DA771]') : ''
            ]"
            @open="open(dm.id)"
            @menu="openMenu(dm, $event)"
            @favourite="actions.toggleFavourite(dm)"
            @move="actions.move(dm, $event)"
            @dragstart="dragging = dm.id"
            @dragover="onDragOver($event, dm.id)"
            @dragend="onDragEnd"
          />
        </div>
        <p v-if="lists.recent.length" class="px-2 pb-0.5 pt-2 text-xs font-medium text-mnema-tertiary">{{ $t('dm.recent') }}</p>
      </template>

      <DmListRow
        v-for="dm in recentShown"
        :key="dm.id"
        :dm="dm"
        :enhanced="enhanced"
        :open="isOpen(dm.id)"
        :unread="unreadOf(dm.id)"
        :mentions="mentionsOf(dm.id)"
        :muted="isMuted(dm.id)"
        @open="open(dm.id)"
        @menu="openMenu(dm, $event)"
        @favourite="actions.toggleFavourite(dm)"
        @hide="actions.hide(dm)"
      />

      <p v-if="isEmpty" class="truncate px-2 py-1 text-sm italic text-mnema-tertiary" data-testid="dm-empty">{{ $t('dm.empty') }}</p>
      <button
        v-if="hiddenCount > 0"
        type="button"
        class="w-full rounded-md px-2 py-1 text-left text-xs text-mnema-tertiary transition hover:text-mnema-text focus-ring max-md:h-11"
        data-testid="dm-more"
        @click="showAll = true"
      >{{ $t('dm.showMore', { n: hiddenCount }) }}</button>
    </div>

    <ContextMenu v-if="enhanced" v-model="menu.open" :x="menu.x" :y="menu.y" :items="menu.items" />
    <DmCreateDialog v-if="creating" @close="creating = false" />
  </section>
</template>
