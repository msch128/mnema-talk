<script setup>
import { ref, computed, watch, nextTick, onBeforeUnmount } from 'vue'
import { ShieldCheck, Crown, Plus, ChevronDown, X, Hash, Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import CreateChannelModal from './CreateChannelModal.vue'
import EditNameDialog from './EditNameDialog.vue'
import ContextMenu from './ContextMenu.vue'
import SidebarChannelRow from './SidebarChannelRow.vue'
import SidebarCategoryHeader from './SidebarCategoryHeader.vue'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'
import { loadCollapsed, saveCollapsed } from '../lib/channelTree'
import {
  locateChannel, locateCategory, moveChannel, moveCategory, resolveChannelDrop, resolveCategoryDrop
} from '../lib/channelLayout'
import { currentRoute, navigate } from '../lib/router'
import { useMenuState, buildChannelItems, buildCategoryItems, buildMemberItems } from '../composables/useNavMenus'
import { useDismissable } from '../composables/useDismissable'
import { useChannelLayout } from '../composables/useChannelLayout'
import { useSortableDrag } from '../composables/useSortableDrag'

const SERVER_NAME = 'Mnema Talk'
const UNCATEGORIZED = '__uncategorized'
// A collapsed category opens while a channel is held over it this long.
const PEEK_MS = 600
const FLASH_MS = 1200

const emit = defineEmits(['open-admin', 'open-legal'])

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()
const toasts = useToastStore()
const { joinVoiceChannel } = useWebRTC()
// The order on screen: the server's, or a move that is still being saved.
const { layout, commit } = useChannelLayout()

const showCreateChannelModal = ref(false)
const modalChannelType = ref('text')
const modalCategoryId = ref('')

function openCreateChannel(type = 'text', categoryId = '') {
  modalChannelType.value = type
  modalCategoryId.value = categoryId
  showCreateChannelModal.value = true
}

async function handleDeleteChannel(channel) {
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
    toasts.error(err.message || t('sidebar.deleteFailed'))
  }
}

async function handleDeleteCategory(category) {
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
    toasts.error(err.message || t('sidebar.deleteFailed'))
  }
}

// ---- Context menus (channels, categories, voice participants) ----

const menu = useMenuState()
const editing = ref(null) // { kind: 'channel' | 'category', entity }
const menuHandlers = {
  onEdit: entity => { editing.value = { kind: entity.channels ? 'category' : 'channel', entity } },
  onDelete: entity => (entity.channels ? handleDeleteCategory(entity) : handleDeleteChannel(entity))
}

function openChannelMenu(e, channel) {
  menu.show(e, () => buildChannelItems(channel, menuHandlers))
}

function openCategoryMenu(e, category) {
  menu.show(e, () => buildCategoryItems(category, menuHandlers))
}

function openMemberMenu(e, user) {
  menu.show(e, refresh => buildMemberItems(user, { refresh }))
}

// ---- Channel tree (uncategorized first, then categories in sort order) ----

const sections = computed(() => {
  const tree = layout.value
  return tree.uncategorized.length
    ? [{ id: UNCATEGORIZED, headless: true, channels: tree.uncategorized }, ...tree.categories]
    : tree.categories
})

const collapsed = ref(new Set(loadCollapsed()))
// Collapsed categories opened for the moment while a channel is dragged over them.
const peeked = ref(new Set())

function setCollapsed(ids) {
  collapsed.value = new Set(ids)
  saveCollapsed([...collapsed.value])
}

function toggleCategory(id) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  setCollapsed(next)
}

function expandCategory(id) {
  if (!collapsed.value.has(id)) return
  const next = new Set(collapsed.value)
  next.delete(id)
  setCollapsed(next)
}

// Whether a category's channels are hidden right now.
function isCollapsed(id) {
  return collapsed.value.has(id) && !peeked.value.has(id)
}

// A collapsed category still shows the selected text channel
// and the voice channel you're connected to. A dragged category shows none.
function visibleChannels(category) {
  if (category.headless) return category.channels
  if (isDragged('category', category.id)) return []
  if (!isCollapsed(category.id)) return category.channels
  return category.channels.filter(ch =>
    ch.type === 'voice' ? voiceStore.currentChannelId === ch.id : chatStore.activeChannel?.id === ch.id
  )
}

// Guess the type for the category's "+" from what the category already holds.
function defaultTypeFor(category) {
  const chs = category.channels || []
  return chs.length && chs.every(c => c.type === 'voice') ? 'voice' : 'text'
}

async function handleVoiceClick(channel) {
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
  navigate(`/v/${channel.id}${currentRoute.value.showChat ? '/chat' : ''}`)
}

function handleTextClick(channel) {
  chatStore.selectChannel(channel)
  voiceStore.activeView = 'chat'
}

function handleChannelClick(channel) {
  if (channel.type === 'voice') handleVoiceClick(channel)
  else handleTextClick(channel)
}

async function handleVoiceUserClick(channel, user) {
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
// A move shows at once and is saved right away (useChannelLayout); a toast
// offers to undo it. The drop rules live in lib/channelLayout.

const navEl = ref(null)
const flashKey = ref('')
const announcement = ref('')
let flashTimer = null

function itemOf(kind, entity) {
  return { kind, id: entity.id, name: entity.name, type: entity.type }
}

function categoryLabel(categoryId) {
  return categoryId == null ? t('admin.uncategorized') : locateCategory(layout.value, categoryId)?.category.name || ''
}

// Tells screen readers where the item ended up (an identical message is
// announced again, hence the reset).
function announce(text) {
  announcement.value = ''
  nextTick(() => { announcement.value = text })
}

function flash(key) {
  clearTimeout(flashTimer)
  flashKey.value = ''
  nextTick(() => {
    flashKey.value = key
    flashTimer = setTimeout(() => { flashKey.value = '' }, FLASH_MS)
  })
}

function applyMove(item, next) {
  if (next === layout.value) return false
  commit(next, { toast: t(item.kind === 'channel' ? 'sidebar.channelMoved' : 'sidebar.categoryMoved') })
  if (item.kind === 'channel') {
    const at = locateChannel(next, item.id)
    announce(t('sidebar.movedChannel', { name: item.name, position: at.index + 1, total: at.total, category: categoryLabel(at.categoryId) }))
  } else {
    const at = locateCategory(next, item.id)
    announce(t('sidebar.movedCategory', { name: item.name, position: at.index + 1, total: at.total }))
  }
  flash(`${item.kind}:${item.id}`)
  return true
}

// What the pointer is over, measured on the rendered sidebar. Only the
// height counts, so the full width of a row is a target and the gaps between
// rows don't make the indicator jump.
function ratioIn(rect, y) {
  return rect.height ? Math.min(1, Math.max(0, (y - rect.top) / rect.height)) : 0
}

function sectionId(el) {
  const id = el.getAttribute('data-drop-section')
  return id === UNCATEGORIZED ? null : id
}

// The section the pointer is in, or else the last one above it.
function sectionAt(y) {
  let found = null
  for (const el of navEl.value.querySelectorAll('[data-drop-section]')) {
    const rect = el.getBoundingClientRect()
    if (rect.top <= y) found = { el, rect, inside: y < rect.bottom }
  }
  return found
}

function channelHit(y) {
  const section = sectionAt(y)
  if (!section) return { zone: 'start' }
  if (!section.inside) return { zone: 'end', id: sectionId(section.el) }
  // The row (or header) under the pointer, else the nearest one in this section.
  let best = null
  for (const el of section.el.querySelectorAll('[data-drop]')) {
    const rect = el.getBoundingClientRect()
    const distance = y < rect.top ? rect.top - y : y >= rect.bottom ? y - rect.bottom : 0
    if (!best || distance < best.distance) best = { el, rect, distance }
  }
  if (!best) return { zone: 'end', id: sectionId(section.el) }
  const zone = best.el.getAttribute('data-drop')
  const id = best.el.getAttribute('data-id')
  if (zone === 'channel-tail') return { zone: 'channel', id, ratio: 1 }
  return { zone, id, ratio: ratioIn(best.rect, y) }
}

function categoryHit(y) {
  const section = sectionAt(y)
  if (!section) return { zone: 'start' }
  return { zone: 'section', id: sectionId(section.el), ratio: section.inside ? ratioIn(section.rect, y) : 1 }
}

function resolveDrop({ x, y, item }) {
  const nav = navEl.value
  if (!nav) return null
  const r = nav.getBoundingClientRect()
  // Outside the channel list: dropping there cancels.
  if (x < r.left || x > r.right || y < r.top || y > r.bottom) return null
  if (item.kind === 'category') return resolveCategoryDrop(layout.value, item.id, categoryHit(y))
  return resolveChannelDrop(layout.value, item.id, channelHit(y), { isCollapsed })
}

function handleDrop(item, target) {
  if (item.kind === 'category') {
    applyMove(item, moveCategory(layout.value, item.id, target.index))
    return
  }
  // A category opened by hovering stays open when the channel went into it.
  if (target.categoryId && peeked.value.has(target.categoryId)) expandCategory(target.categoryId)
  applyMove(item, moveChannel(layout.value, item.id, target.categoryId, target.index))
}

// Touch: holding an item and letting go without moving opens its menu.
function handleLongPress(item, e) {
  if (item.kind === 'channel') {
    const at = locateChannel(layout.value, item.id)
    if (at) openChannelMenu(e, at.channel)
  } else {
    const at = locateCategory(layout.value, item.id)
    if (at) openCategoryMenu(e, at.category)
  }
}

const drag = useSortableDrag({
  enabled: () => authStore.isAdmin,
  scrollContainer: navEl,
  // Targets are found by height (channelHit), not by the element under the pointer.
  hitTest: () => null,
  resolve: resolveDrop,
  onDrop: handleDrop,
  onEnd: () => closePeeks(),
  onLongPress: handleLongPress
})

const dragItem = computed(() => (drag.state.phase === 'dragging' ? drag.state.item : null))

function startDrag(e, kind, entity) {
  if (authStore.isAdmin) drag.pointerDown(e, itemOf(kind, entity))
}

function isDragged(kind, id) {
  return dragItem.value?.kind === kind && dragItem.value.id === id
}

function indicatorFor(key) {
  const target = dragItem.value && drag.state.target
  return target?.indicator?.key === key ? target.indicator.edge : ''
}

// Holding a channel over a collapsed category opens it after a moment.
let peekTimer = null
let peekFor = null

watch(() => drag.state.target, target => {
  const id = target?.indicator?.edge === 'inside' ? target.categoryId : null
  if (id === peekFor) return
  clearTimeout(peekTimer)
  peekFor = id
  if (!id) return
  peekTimer = setTimeout(() => {
    peeked.value = new Set([...peeked.value, id])
    nextTick(() => drag.refresh())
  }, PEEK_MS)
})

function closePeeks() {
  clearTimeout(peekTimer)
  peekTimer = null
  peekFor = null
  if (peeked.value.size) peeked.value = new Set()
}

const ghostStyle = computed(() => {
  const touch = drag.state.pointerType === 'touch'
  const x = drag.state.x + (touch ? -24 : 14)
  const y = drag.state.y + (touch ? -56 : 10)
  return { transform: `translate3d(${x}px, ${y}px, 0)` }
})

// Members never get the touch listener, so their scrolling stays passive.
const navListeners = computed(() => (authStore.isAdmin ? { touchmove: drag.touchMove } : {}))

watch(() => authStore.isAdmin, admin => { if (!admin) drag.cancel() })

onBeforeUnmount(() => {
  clearTimeout(flashTimer)
  clearTimeout(peekTimer)
})

// ---- Server header dropdown ----

const menuOpen = ref(false)
const menuRoot = ref(null)
const menuButton = ref(null)
const menuEl = ref(null)

function menuItems() {
  return menuEl.value ? [...menuEl.value.querySelectorAll('[role="menuitem"]')] : []
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

function onMenuButtonKeydown(e) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    openMenu(0)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    openMenu(-1)
  }
}

function onMenuKeydown(e) {
  const items = menuItems()
  const i = items.indexOf(document.activeElement)
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

function runMenuAction(action) {
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
      :class="['flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-2 pt-3 pb-4', authStore.isAdmin && '[-webkit-touch-callout:none]']"
      :aria-label="$t('sidebar.channels')"
      v-on="navListeners"
    >
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
          @toggle="toggleCategory(category.id)"
          @menu="openCategoryMenu($event, category)"
          @create-channel="openCreateChannel(defaultTypeFor(category), category.id)"
          @delete="handleDeleteCategory(category)"
          @drag-start="startDrag($event, 'category', category)"
        />

        <div class="mt-0.5">
          <SidebarChannelRow
            v-for="channel in visibleChannels(category)"
            :key="channel.id"
            :channel="channel"
            :admin="authStore.isAdmin"
            :dragging="isDragged('channel', channel.id)"
            :indicator="indicatorFor(`channel:${channel.id}`)"
            :flash="flashKey === `channel:${channel.id}`"
            @open="handleChannelClick(channel)"
            @menu="openChannelMenu($event, channel)"
            @delete="handleDeleteChannel(channel)"
            @drag-start="startDrag($event, 'channel', channel)"
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
    </nav>

    <!-- Where a move ended up, for screen readers -->
    <div class="sr-only" role="status" aria-live="polite" data-testid="sidebar-announcer">{{ announcement }}</div>

    <!-- The dragged item follows the pointer -->
    <Teleport to="body">
      <div
        v-if="dragItem"
        class="fixed left-0 top-0 z-[70] pointer-events-none max-w-[240px] h-8 px-2.5 flex items-center gap-1.5 rounded-md bg-mnema-elevated border border-mnema-border shadow-xl text-sm text-mnema-text"
        :style="ghostStyle"
        data-testid="drag-ghost"
        aria-hidden="true"
      >
        <ChevronDown v-if="dragItem.kind === 'category'" class="w-3.5 h-3.5 flex-shrink-0 text-mnema-tertiary" />
        <Volume2 v-else-if="dragItem.type === 'voice'" class="w-4 h-4 flex-shrink-0 text-mnema-tertiary" />
        <Hash v-else class="w-4 h-4 flex-shrink-0 text-mnema-tertiary" />
        <span :class="['truncate', dragItem.kind === 'category' && 'text-xs font-semibold uppercase tracking-wide']">{{ dragItem.name }}</span>
      </div>
    </Teleport>

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
