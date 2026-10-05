<script setup>
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { Hash, Volume2, ShieldCheck, Crown, Plus, Trash2, ChevronDown, ChevronRight, X } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import CreateChannelModal from './CreateChannelModal.vue'
import UserAvatar from './UserAvatar.vue'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'
import { buildChannelTree, loadCollapsed, saveCollapsed } from '../lib/channelTree'

const SERVER_NAME = 'Mnema Talk'

const emit = defineEmits(['open-admin', 'open-legal'])

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()
const toasts = useToastStore()
const { joinVoiceChannel } = useWebRTC()

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

// ---- Channel tree (uncategorized first, then categories in sort order) ----

const sections = computed(() => {
  const tree = buildChannelTree(chatStore.categories, chatStore.uncategorized)
  const list = tree.categories
  return tree.uncategorized.length
    ? [{ id: '__uncategorized', headless: true, channels: tree.uncategorized }, ...list]
    : list
})

const collapsed = ref(new Set(loadCollapsed()))

function toggleCategory(id) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
  saveCollapsed([...next])
}

function isTextActive(channel) {
  return chatStore.activeChannel?.id === channel.id && voiceStore.activeView === 'chat'
}

function isVoiceActive(channel) {
  return voiceStore.currentChannelId === channel.id && voiceStore.activeView === 'voice'
}

// A collapsed category still shows the selected text channel
// and the voice channel you're connected to.
function visibleChannels(category) {
  if (category.headless || !collapsed.value.has(category.id)) return category.channels
  return category.channels.filter(ch =>
    ch.type === 'voice' ? voiceStore.currentChannelId === ch.id : chatStore.activeChannel?.id === ch.id
  )
}

// Guess the type for the category's "+" from what the category already holds.
function defaultTypeFor(category) {
  const chs = category.channels || []
  return chs.length && chs.every(c => c.type === 'voice') ? 'voice' : 'text'
}

function voiceUsers(channel) {
  const users = voiceStore.channelUsers[channel.id]
  return users ? Object.values(users) : []
}

function handleVoiceClick(channel) {
  // Select channel messages for side-chat
  chatStore.selectChannel(channel)
  // Join voice and switch to the Tafelrunde
  joinVoiceChannel(channel.id)
  voiceStore.activeView = 'voice'
}

function handleTextClick(channel) {
  chatStore.selectChannel(channel)
  voiceStore.activeView = 'chat'
}

function handleChannelClick(channel) {
  if (channel.type === 'voice') handleVoiceClick(channel)
  else handleTextClick(channel)
}

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
  if (e.key === 'Escape') {
    e.preventDefault()
    closeMenu(true)
  } else if (e.key === 'ArrowDown') {
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

function onDocumentPointerDown(e) {
  if (menuOpen.value && menuRoot.value && !menuRoot.value.contains(e.target)) closeMenu()
}

function onDocumentKeydown(e) {
  if (e.key === 'Escape' && menuOpen.value) closeMenu(true)
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  document.addEventListener('keydown', onDocumentKeydown)
})

onUnmounted(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  document.removeEventListener('keydown', onDocumentKeydown)
})

watch(() => authStore.isAdmin, () => closeMenu())

function unreadCount(channel) {
  if (channel.type === 'voice') return 0
  return chatStore.readStates[channel.id]?.unread_count || 0
}

function mentionCount(channel) {
  if (channel.type === 'voice') return 0
  return chatStore.readStates[channel.id]?.mention_count || 0
}

// Shared row styling (34px channel rows, rounded hover/selected states).
const rowBase = 'relative w-full h-[34px] flex items-center justify-between gap-1.5 px-2 mb-px rounded-md text-nav transition-colors group cursor-pointer text-left min-w-0'
function rowClass(active, unread = false) {
  return [
    rowBase,
    active
      ? 'bg-mnema-hover text-mnema-text font-medium shadow-[inset_2px_0_0_0_#2DA771]'
      : unread
        ? 'text-mnema-text font-semibold hover:bg-mnema-hover/70'
        : 'text-mnema-muted hover:bg-mnema-hover/70 hover:text-mnema-text'
  ]
}
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
    <nav class="flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-2 pt-3 pb-4" :aria-label="$t('sidebar.channels')">
      <!-- Uncategorized channels first (headless section), then the categories -->
      <section
        v-for="category in sections"
        :key="category.id"
        :class="[category.headless ? 'mb-1' : 'mt-3 first:mt-0', 'group/cat']"
        :data-category-id="category.headless ? undefined : category.id"
      >
        <div v-if="!category.headless" class="h-6 flex items-center gap-1 pr-1">
          <button
            type="button"
            class="flex-1 min-w-0 h-6 pl-0.5 flex items-center gap-0.5 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary hover:text-mnema-muted transition-colors focus:outline-none focus-visible:text-mnema-text"
            :aria-expanded="collapsed.has(category.id) ? 'false' : 'true'"
            @click="toggleCategory(category.id)"
          >
            <ChevronRight v-if="collapsed.has(category.id)" class="w-3 h-3 flex-shrink-0" />
            <ChevronDown v-else class="w-3 h-3 flex-shrink-0" />
            <span class="truncate">{{ category.name }}</span>
          </button>
          <div
            v-if="authStore.isAdmin"
            class="flex items-center gap-0.5 flex-shrink-0 opacity-0 group-hover/cat:opacity-100 focus-within:opacity-100 transition"
          >
            <button
              @click.stop="openCreateChannel(defaultTypeFor(category), category.id)"
              v-tooltip="$t('channel.create')"
              class="w-5 h-5 flex items-center justify-center rounded text-mnema-tertiary hover:text-mnema-text transition"
            >
              <Plus class="w-4 h-4" />
            </button>
            <button
              @click.stop="handleDeleteCategory(category)"
              v-tooltip="$t('sidebar.deleteCategory')"
              class="w-5 h-5 flex items-center justify-center rounded text-mnema-tertiary hover:text-mnema-danger transition"
            >
              <Trash2 class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <div class="mt-0.5">
          <template v-for="channel in visibleChannels(category)" :key="channel.id">
            <div
              :class="rowClass(
                channel.type === 'voice' ? isVoiceActive(channel) : isTextActive(channel),
                unreadCount(channel) > 0
              )"
              role="button"
              tabindex="0"
              :data-channel-type="channel.type"
              @click="handleChannelClick(channel)"
              @keydown.enter.self.prevent="handleChannelClick(channel)"
              @keydown.space.self.prevent="handleChannelClick(channel)"
            >
              <!-- Unread pip on left edge -->
              <span
                v-if="unreadCount(channel) > 0 && !isTextActive(channel)"
                class="absolute -left-1.5 w-1 h-2 rounded-r bg-mnema-text"
              ></span>

              <div class="flex items-center gap-1.5 min-w-0">
                <Volume2
                  v-if="channel.type === 'voice'"
                  :class="['w-5 h-5 flex-shrink-0 transition-colors', voiceStore.currentChannelId === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text']"
                />
                <Hash
                  v-else
                  :class="[
                    'w-5 h-5 flex-shrink-0 transition-colors',
                    chatStore.activeChannel?.id === channel.id
                      ? 'text-mnema-accent'
                      : unreadCount(channel) > 0
                        ? 'text-mnema-text'
                        : 'text-mnema-tertiary group-hover:text-mnema-text'
                  ]"
                />
                <span class="truncate">{{ channel.name }}</span>
              </div>

              <!-- Unread & Mention Badges -->
              <div class="flex items-center gap-1 ml-auto flex-shrink-0">
                <span
                  v-if="mentionCount(channel) > 0"
                  class="px-1.5 py-0.5 rounded-full bg-mnema-danger text-white text-xs font-bold leading-none min-w-[18px] text-center"
                >
                  {{ mentionCount(channel) }}
                </span>
                <span
                  v-else-if="unreadCount(channel) > 0 && !isTextActive(channel)"
                  class="px-1.5 py-0.5 rounded-full bg-mnema-surface border border-mnema-border text-mnema-text text-xs font-semibold leading-none min-w-[18px] text-center"
                >
                  {{ unreadCount(channel) }}
                </span>
              </div>

              <button
                v-if="authStore.isAdmin"
                @click.stop="handleDeleteChannel(channel)"
                v-tooltip="channel.type === 'voice' ? $t('sidebar.deleteVoiceChannel') : $t('sidebar.deleteChannel')"
                class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 w-6 h-6 flex items-center justify-center rounded text-mnema-tertiary hover:text-mnema-danger transition flex-shrink-0"
              >
                <Trash2 class="w-4 h-4" />
              </button>
            </div>

            <!-- Connected voice users (indented, 24px avatars, speaking ring) -->
            <div v-if="channel.type === 'voice' && voiceUsers(channel).length" class="pl-7 pb-1">
              <div
                v-for="user in voiceUsers(channel)"
                :key="user.id"
                class="h-8 flex items-center gap-2 px-2 rounded-md text-mnema-muted hover:bg-mnema-hover/70 hover:text-mnema-text transition-colors min-w-0"
              >
                <UserAvatar :user="user" size="xs" :is-speaking="!!voiceStore.speakingUsers[user.id]" />
                <span class="truncate text-sm">{{ user.display_name || user.username }}</span>
                <span v-if="user.role === 'admin'" class="text-xs px-1 rounded bg-amber-500/10 text-amber-400 ml-auto flex-shrink-0">
                  {{ $t('role.admin') }}
                </span>
              </div>
            </div>
          </template>

          <div
            v-if="!category.headless && !category.channels.length && !collapsed.has(category.id)"
            class="px-2 py-1 text-sm text-mnema-tertiary italic truncate"
          >
            {{ $t('sidebar.noChannels') }}
          </div>
        </div>
      </section>
    </nav>

    <!-- Create Channel Modal Dialog -->
    <CreateChannelModal
      v-if="showCreateChannelModal"
      :initial-type="modalChannelType"
      :initial-category-id="modalCategoryId"
      @close="showCreateChannelModal = false"
    />
  </aside>
</template>
