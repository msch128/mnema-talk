<script setup lang="ts">
import type { Channel, ChannelType, Category, VoiceUser } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
type MenuOpener = MouseEvent | KeyboardEvent | { currentTarget: HTMLElement | null; preventDefault?: () => void }
interface EditingEntity { kind: 'channel' | 'category'; entity: Channel | Category | null; after?: string | null }
type SidebarSection = (Category & { headless?: false }) | { id: string; headless: true; channels: Channel[] }
import { ref, computed, watch, nextTick } from 'vue'
import { ShieldCheck, Crown, Plus, FolderPlus, ChevronDown, X } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import CreateChannelModal from './CreateChannelModal.vue'
import EditNameDialog from './EditNameDialog.vue'
import ContextMenu from './ContextMenu.vue'
import SidebarChannelRow from './SidebarChannelRow.vue'
import SidebarCategoryHeader from './SidebarCategoryHeader.vue'
import SidebarDragGhost from './SidebarDragGhost.vue'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'
import { loadCollapsed, saveCollapsed } from '../lib/channelTree'
import { locateCategory, moveCategory } from '../lib/channelLayout'
import { currentRoute, navigate } from '../lib/router'
import {
  useMenuState, buildChannelItems, buildCategoryItems, buildMemberItems, buildSidebarItems
} from '../composables/useNavMenus'
import { useDismissable } from '../composables/useDismissable'
import { useChannelLayout } from '../composables/useChannelLayout'
import { useSidebarReorder, UNCATEGORIZED_SECTION } from '../composables/useSidebarReorder'

const SERVER_NAME = 'Mnema Talk'

const emit = defineEmits<{ 'open-admin': []; 'open-legal': [] }>()

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()
const toasts = useToastStore()
const { joinVoiceChannel } = useWebRTC()
// The order on screen: the server's, or a move that is still being saved.
const { layout, commit } = useChannelLayout()

const showCreateChannelModal = ref(false)
const modalChannelType = ref<ChannelType>('text')
const modalCategoryId = ref('')

function openCreateChannel(type: ChannelType = 'text', categoryId = '') {
  modalChannelType.value = type
  modalCategoryId.value = categoryId
  showCreateChannelModal.value = true
}

async function handleDeleteChannel(channel: Channel) {
  const isVoice = channel.type === 'voice'
  const ok = await confirm({
    title: t(isVoice ? 'sidebar.deleteVoiceChannelTitle' : 'sidebar.deleteChannelTitle', { name: channel.name }),
    body: t(isVoice ? 'sidebar.deleteVoiceChannelBody' : 'sidebar.deleteChannelBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await chatStore.deleteChannel(channel.id)
    toasts.success(t('sidebar.channelDeleted'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('sidebar.deleteFailed')))
  }
}

async function handleDeleteCategory(category: Category) {
  const ok = await confirm({
    title: t('sidebar.deleteCategoryTitle', { name: category.name }),
    body: t('sidebar.deleteCategoryBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await chatStore.deleteCategory(category.id)
    toasts.success(t('sidebar.categoryDeleted'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('sidebar.deleteFailed')))
  }
}

async function handleDuplicateChannel(channel: Channel) {
  try {
    const created = await chatStore.duplicateChannel(channel.id)
    toasts.success(t('sidebar.channelDuplicated', { name: channel.name }))
    // The copy sits right below the original; show it without navigating.
    if (created?.id) reveal('channel', created.id)
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('sidebar.duplicateFailed')))
  }
}

// New categories go to the end; one created from a category's menu then
// moves right below that category (a silent layout save).
function openCreateCategory(afterCategoryId: string | null = null) {
  editing.value = { kind: 'category', entity: null, after: afterCategoryId }
}

function handleCategoryCreated(category: Category) {
  const after = editing.value?.after
  if (!category?.id) return
  const anchor = after && locateCategory(layout.value, after)
  if (anchor) commit(moveCategory(layout.value, category.id, anchor.index + 1))
  reveal('category', category.id)
}

// ---- Context menus (channels, categories, voice participants, the list) ----

const menu = useMenuState()
const editing = ref<EditingEntity | null>(null) // { kind: 'channel' | 'category', entity (null: create), after }
const menuHandlers = {
  onEdit: (entity: Channel | Category) => { editing.value = { kind: 'channels' in entity ? 'category' : 'channel', entity } },
  onDelete: (entity: Channel | Category) => ('channels' in entity ? handleDeleteCategory(entity) : handleDeleteChannel(entity)),
  onDuplicate: (channel: Channel) => handleDuplicateChannel(channel),
  onCreateChannel: (category?: Category | null) => openCreateChannel(category ? defaultTypeFor(category) : 'text', category?.id || ''),
  onCreateCategory: (category?: Category | null) => openCreateCategory(category?.id ?? null),
  onCollapseAll: () => setCollapsed(layout.value.categories.map(c => c.id)),
  onExpandAll: () => setCollapsed([])
}

function collapseState() {
  const ids = layout.value.categories.map(c => c.id)
  return {
    allCollapsed: ids.every(id => collapsed.value.has(id)),
    noneCollapsed: !ids.some(id => collapsed.value.has(id))
  }
}

function openChannelMenu(e: MenuOpener, channel: Channel) {
  menu.show(e, () => buildChannelItems(channel, menuHandlers))
}

function openCategoryMenu(e: MenuOpener, category: Category) {
  menu.show(e, () => buildCategoryItems(category, { ...menuHandlers, ...collapseState() }))
}

// The empty part of the channel list: admins can create things there,
// everyone else keeps the browser's menu.
function openListMenu(e: MouseEvent) {
  if (!buildSidebarItems(menuHandlers).length) return
  menu.show(e, () => buildSidebarItems(menuHandlers))
}

function openMemberMenu(e: MouseEvent | KeyboardEvent, user: VoiceUser) {
  menu.show(e, refresh => buildMemberItems(user, { refresh }))
}

// ---- Channel tree (uncategorized first, then categories in sort order) ----

const sections = computed<SidebarSection[]>(() => {
  const tree = layout.value
  return tree.uncategorized.length
    ? [{ id: UNCATEGORIZED_SECTION, headless: true, channels: tree.uncategorized }, ...tree.categories]
    : tree.categories
})

const collapsed = ref(new Set(loadCollapsed()))

function setCollapsed(ids: Iterable<string>) {
  collapsed.value = new Set(ids)
  saveCollapsed([...collapsed.value])
}

function toggleCategory(id: string) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  setCollapsed(next)
}

function expandCategory(id: string) {
  if (!collapsed.value.has(id)) return
  const next = new Set(collapsed.value)
  next.delete(id)
  setCollapsed(next)
}

// A collapsed category still shows the selected text channel
// and the voice channel you're connected to. A dragged category shows none.
function visibleChannels(category: SidebarSection) {
  if ('headless' in category && category.headless) return category.channels
  if (isDragged('category', category.id)) return []
  if (!isCollapsed(category.id)) return category.channels
  return category.channels.filter(ch =>
    ch.type === 'voice' ? voiceStore.currentChannelId === ch.id : chatStore.activeChannel?.id === ch.id
  )
}

// Guess the type for the category's "+" from what the category already holds.
function defaultTypeFor(category: Category) {
  const chs = category.channels || []
  return chs.length && chs.every(c => c.type === 'voice') ? 'voice' : 'text'
}

async function handleVoiceClick(channel: Channel) {
  if (voiceStore.warnSwitchChannel && voiceStore.currentChannelId && voiceStore.currentChannelId !== channel.id) {
    const ok = await confirm({
      title: t('audio.switchChannelTitle'),
      body: t('audio.switchChannelPrompt', { channel: channel.name }),
      confirmLabel: t('audio.switchChannelConfirm'),
      cancelLabel: t('common.cancel'),
      danger: false
    })
    if (!ok) return
  }
  // Select channel messages for side-chat
  chatStore.selectChannel(channel)
  // Join voice and switch to the Talk
  joinVoiceChannel(channel.id)
  voiceStore.activeView = 'voice'
  // /v/:id keeps the Talk's chat toggle as it is.
  navigate(`/v/${channel.id}${currentRoute.value.view === 'voice' && currentRoute.value.showChat ? '/chat' : ''}`)
}

function handleTextClick(channel: Channel) {
  chatStore.selectChannel(channel)
  voiceStore.activeView = 'chat'
}

function handleChannelClick(channel: Channel) {
  if (channel.type === 'voice') handleVoiceClick(channel)
  else handleTextClick(channel)
}

async function handleVoiceUserClick(channel: Channel, user: VoiceUser) {
  if (voiceStore.mediaState[user.id]?.screen) {
    if (voiceStore.currentChannelId !== channel.id) {
      await handleVoiceClick(channel)
    } else {
      voiceStore.activeView = 'voice'
    }
    voiceStore.watchScreen(user.id)
    return
  }
  chatStore.openUserProfile(user)
}

// ---- Moving channels and categories (admins) ----
//
// Drag and drop and Alt+Arrow (useSidebarReorder); every move shows at once,
// is saved right away and can be undone from a toast (useChannelLayout).

const {
  navEl, hintId, flashKey, announcement, isCollapsed, reveal, moveByKey,
  drag, dragItem, startDrag, isDragged, indicatorFor, navListeners
} = useSidebarReorder({
  layout,
  commit,
  collapsed,
  expandCategory,
  enabled: () => authStore.isAdmin,
  // Touch: holding an item and letting go without moving opens its menu.
  onLongPress: (_kind: 'channel' | 'category', entity: Channel | Category, e: PointerEvent) => ('channels' in entity ? openCategoryMenu(e, entity) : openChannelMenu(e, entity))
})

// Channel rows accept boundary drops; inside drops belong to categories.
function channelIndicator(id: string): '' | 'top' | 'bottom' {
  const indicator = indicatorFor(`channel:${id}`)
  return indicator === 'inside' ? '' : indicator
}

// ---- Server header dropdown ----

const menuOpen = ref(false)
const menuRoot = ref<HTMLElement | null>(null)
const menuButton = ref<HTMLButtonElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)

function menuItems() {
  return menuEl.value ? [...menuEl.value.querySelectorAll<HTMLElement>('[role="menuitem"]')] : []
}

function openMenu(focusIndex = 0) {
  menuOpen.value = true
  nextTick(() => {
    const items = menuItems()
    items[(focusIndex + items.length) % items.length]?.focus()
  })
}

function closeMenu(returnFocus = false) {
  if (!menuOpen.value) return
  menuOpen.value = false
  if (returnFocus) menuButton.value?.focus()
}

function toggleMenu() {
  if (menuOpen.value) closeMenu()
  else openMenu()
}

function onMenuButtonKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    openMenu(0)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    openMenu(-1)
  }
}

function onMenuKeydown(e: KeyboardEvent) {
  const items = menuItems()
  const i = items.findIndex(el => el === document.activeElement)
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    items[(i + 1) % items.length]?.focus()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    items[(i - 1 + items.length) % items.length]?.focus()
  } else if (e.key === 'Home') {
    e.preventDefault()
    items[0]?.focus()
  } else if (e.key === 'End') {
    e.preventDefault()
    items[items.length - 1]?.focus()
  } else if (e.key === 'Tab') {
    closeMenu()
  }
}

function runMenuAction(action: () => void) {
  closeMenu(true)
  action()
}

// A press elsewhere closes the menu; Escape also returns focus to its button.
useDismissable(menuRoot, (e, reason) => closeMenu(reason === 'escape'), { active: menuOpen })

watch(() => authStore.isAdmin, () => closeMenu())

const menuItemClass = 'w-full h-8 px-2 flex items-center justify-between gap-3 rounded text-sm text-left text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text focus:outline-none focus-visible:bg-mnema-hover focus-visible:text-mnema-text transition-colors'
</script>

<template>
  <aside class="w-full min-w-0 bg-mnema-raised flex flex-col h-full select-none">
    <!-- Server Header (48px, aligned with the channel header) with dropdown menu -->
    <div ref="menuRoot" class="relative flex-shrink-0">
      <button
        ref="menuButton"
        type="button"
        aria-haspopup="menu"
        :aria-expanded="menuOpen ? 'true' : 'false'"
        aria-controls="server-menu"
        :class="[
          'w-full h-12 pl-3 pr-3 border-b border-mnema-hairline flex items-center gap-2.5 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent',
          menuOpen ? 'bg-mnema-hover' : 'hover:bg-mnema-hover/70'
        ]"
        v-tooltip.visual="SERVER_NAME"
        @click="toggleMenu"
        @keydown="onMenuButtonKeydown"
      >
        <!-- Mnema Forest Mark -->
        <span class="w-7 h-7 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-sm shadow-sm flex-shrink-0">
          M
        </span>
        <span class="flex flex-col min-w-0 flex-1">
          <span class="font-semibold text-nav text-mnema-text truncate">{{ SERVER_NAME }}</span>
          <span class="text-xs text-mnema-tertiary truncate">{{ $t('sidebar.subtitle') }}</span>
        </span>
        <X v-if="menuOpen" class="w-4 h-4 text-mnema-muted flex-shrink-0" />
        <ChevronDown v-else class="w-4 h-4 text-mnema-muted flex-shrink-0" />
      </button>

      <div
        v-if="menuOpen"
        id="server-menu"
        ref="menuEl"
        role="menu"
        :aria-label="$t('sidebar.menu')"
        class="absolute left-2 top-[52px] z-40 w-max min-w-[calc(100%-16px)] max-w-[calc(100vw-16px)] p-1.5 rounded-lg bg-mnema-elevated border border-mnema-border shadow-xl"
        @keydown="onMenuKeydown"
      >
        <template v-if="authStore.isAdmin">
          <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('open-admin'))">
            <span class="truncate">{{ $t('menu.adminConsole') }}</span>
            <Crown class="w-4 h-4 text-mnema-amber flex-shrink-0" />
          </button>
          <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => openCreateChannel('text'))">
            <span class="truncate">{{ $t('channel.create') }}</span>
            <Plus class="w-4 h-4 flex-shrink-0" />
          </button>
          <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => openCreateCategory())">
            <span class="truncate">{{ $t('sidebar.createCategory') }}</span>
            <FolderPlus class="w-4 h-4 flex-shrink-0" />
          </button>
          <div class="my-1 h-px bg-mnema-hairline" role="separator"></div>
        </template>
        <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('open-legal'))">
          <span class="truncate">{{ $t('menu.legal') }}</span>
          <ShieldCheck class="w-4 h-4 flex-shrink-0" />
        </button>
      </div>
    </div>

    <!-- Navigation Scroll Area -->
    <nav
      ref="navEl"
      :class="['flex-1 min-h-0 flex flex-col overflow-y-auto overflow-x-hidden px-2 pt-3', authStore.isAdmin && '[-webkit-touch-callout:none]']"
      :aria-label="$t('sidebar.channels')"
      v-on="navListeners"
      @contextmenu="openListMenu"
    >
      <div class="flex-shrink-0">
        <!-- Uncategorized channels first (headless section), then the categories -->
        <section
          v-for="category in sections"
          :key="category.id"
          :class="[category.headless ? 'mb-1' : 'mt-3 first:mt-0', 'relative group/cat']"
          :data-category-id="category.headless ? undefined : category.id"
          :data-drop-section="category.id"
        >
          <SidebarCategoryHeader
            v-if="!category.headless"
            :category="category"
            :collapsed="isCollapsed(category.id)"
            :admin="authStore.isAdmin"
            :dragging="isDragged('category', category.id)"
            :indicator="indicatorFor(`header:${category.id}`)"
            :flash="flashKey === `category:${category.id}`"
            :hint-id="hintId"
            @toggle="toggleCategory(category.id)"
            @menu="openCategoryMenu($event, category)"
            @create-channel="openCreateChannel(defaultTypeFor(category), category.id)"
            @delete="handleDeleteCategory(category)"
            @drag-start="startDrag($event, 'category', category)"
            @move="moveByKey('category', category, $event)"
          />

          <div class="mt-0.5">
            <SidebarChannelRow
              v-for="channel in visibleChannels(category)"
              :key="channel.id"
              :channel="channel"
              :admin="authStore.isAdmin"
              :dragging="isDragged('channel', channel.id)"
              :indicator="channelIndicator(channel.id)"
              :flash="flashKey === `channel:${channel.id}`"
              :hint-id="hintId"
              @open="handleChannelClick(channel)"
              @menu="openChannelMenu($event, channel)"
              @delete="handleDeleteChannel(channel)"
              @drag-start="startDrag($event, 'channel', channel)"
              @move="moveByKey('channel', channel, $event)"
              @voice-user-click="handleVoiceUserClick(channel, $event)"
              @member-menu="openMemberMenu"
            />

            <div
              v-if="!category.headless && !category.channels.length && !isCollapsed(category.id) && !isDragged('category', category.id)"
              class="px-2 py-1 text-sm text-mnema-tertiary italic truncate"
              data-drop="empty"
              :data-id="category.id"
            >
              {{ $t('sidebar.noChannels') }}
            </div>
          </div>

          <span
            v-if="indicatorFor(`section:${category.id}`) === 'bottom'"
            class="drop-line -bottom-1.5"
            data-drop-indicator
            aria-hidden="true"
          ></span>
        </section>
      </div>

      <!-- The empty rest of the list (at least a little, even when it
           scrolls): right-click here to create something, drop here to
           append to the last group. -->
      <div class="flex-1 min-h-12" data-drop-tail aria-hidden="true"></div>
    </nav>

    <!-- How to move things, and where a move ended up, for screen readers -->
    <p v-if="authStore.isAdmin" :id="hintId" class="sr-only">{{ $t('sidebar.reorderHint') }}</p>
    <div class="sr-only" role="status" aria-live="polite" data-testid="sidebar-announcer">{{ announcement }}</div>

    <!-- The dragged item follows the pointer -->
    <SidebarDragGhost
      v-if="dragItem"
      :item="dragItem"
      :x="drag.state.x"
      :y="drag.state.y"
      :touch="drag.state.pointerType === 'touch'"
    />

    <ContextMenu
      v-model="menu.state.open"
      :x="menu.state.x"
      :y="menu.state.y"
      :anchor="menu.state.anchor"
      :items="menu.items.value"
    />

    <EditNameDialog
      v-if="editing"
      :kind="editing.kind"
      :entity="editing.entity"
      @created="handleCategoryCreated"
      @close="editing = null"
    />

    <!-- Create Channel Modal Dialog -->
    <CreateChannelModal
      v-if="showCreateChannelModal"
      :initial-type="modalChannelType"
      :initial-category-id="modalCategoryId"
      @close="showCreateChannelModal = false"
    />
  </aside>
</template>
