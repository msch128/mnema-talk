<script setup lang="ts">
import type { Channel, ChannelType, Category, VoiceUser } from '../types/domain'
type MenuOpener = MouseEvent | KeyboardEvent | { currentTarget: HTMLElement | null; preventDefault?: () => void }
interface EditingEntity { kind: 'channel' | 'category'; entity: Channel | Category | null; after?: string | null }
type SidebarSection = (Category & { headless?: false }) | { id: string; headless: true; channels: Channel[] }
import { ref, computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import CreateChannelModal from './CreateChannelModal.vue'
import ChannelPermissionsDialog from './ChannelPermissionsDialog.vue'
import ChannelWebhooksDialog from './ChannelWebhooksDialog.vue'
import { usePermissionsStore } from '../stores/permissions'
import EditNameDialog from './EditNameDialog.vue'
import ContextMenu from './ContextMenu.vue'
import SidebarChannelRow from './SidebarChannelRow.vue'
import SidebarCategoryHeader from './SidebarCategoryHeader.vue'
import SidebarDragGhost from './SidebarDragGhost.vue'
import SidebarServerMenu from './SidebarServerMenu.vue'
import SidebarEventsEntry from './SidebarEventsEntry.vue'
import SidebarDmSection from './SidebarDmSection.vue'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'
import { refuseFullTalk } from '../lib/talks'
import { loadCollapsed, saveCollapsed } from '../lib/channelTree'
import { aggregateActivity } from '../lib/channelActivity'
import { useToastStore } from '../stores/toast'
import { locateCategory, moveCategory } from '../lib/channelLayout'
import { currentRoute, navigate } from '../lib/router'
import { talkPath, textPath } from '../lib/routePaths'
import {
  useMenuState, buildChannelItems, buildCategoryItems, buildMemberItems, buildSidebarItems
} from '../composables/useNavMenus'
import { useCompactLayout } from '../composables/useCompactLayout'
import { useChannelAdminActions } from '../composables/useChannelAdminActions'
import { useChannelLayout } from '../composables/useChannelLayout'
import { useSidebarReorder, UNCATEGORIZED_SECTION } from '../composables/useSidebarReorder'

const emit = defineEmits<{ 'open-admin': []; 'open-legal': [] }>()

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const permissions = usePermissionsStore()
// The channel or category whose permissions are open in a dialog.
const permissionsFor = ref<{ kind: 'channel' | 'category'; entity: Channel | Category } | null>(null)
// The channel whose webhooks are open in a dialog.
const webhooksFor = ref<Channel | null>(null)
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

const { deleteChannel: handleDeleteChannel, deleteCategory: handleDeleteCategory, duplicateChannel: handleDuplicateChannel } = useChannelAdminActions(id => reveal('channel', id))

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
  onWebhooks: (channel: Channel) => { webhooksFor.value = channel },
  onPermissions: (entity: Channel | Category) => { permissionsFor.value = { kind: 'channels' in entity ? 'category' : 'channel', entity } },
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

// What a category's channels have waiting (its header shows it while collapsed).
function activityOf(category: SidebarSection) {
  return aggregateActivity(category.channels || [], chatStore.readStates)
}

async function markEverythingRead() {
  if (await chatStore.markAllRead()) useToastStore().success(t('sidebar.allMarkedRead'))
}

// Guess the type for the category's "+" from what the category already holds.
function defaultTypeFor(category: Category) {
  const chs = category.channels || []
  return chs.length && chs.every(c => c.type === 'voice') ? 'voice' : 'text'
}

// Null when the member backed out of leaving their current Talk, else the started (not yet settled) join.
async function handleVoiceClick(channel: Channel): Promise<{ joined: Promise<void> } | null> {
  if (voiceStore.warnSwitchChannel && voiceStore.currentChannelId && voiceStore.currentChannelId !== channel.id) {
    const ok = await confirm({
      title: t('audio.switchChannelTitle'),
      body: t('audio.switchChannelPrompt', { channel: channel.name }),
      confirmLabel: t('audio.switchChannelConfirm'),
      cancelLabel: t('common.cancel'),
      danger: false
    })
    if (!ok) return null
  }
  if (refuseFullTalk(channel, voiceStore.channelUsers[channel.id], voiceStore.currentChannelId === channel.id || permissions.canIn(channel.id, 'disconnect_members'))) return null
  chatStore.selectChannel(channel) // the Talk's side chat
  const join = Promise.resolve(joinVoiceChannel(channel.id))
  voiceStore.activeView = 'voice'
  // /v/:id keeps the Talk's chat toggle as it is.
  navigate(talkPath(channel, { chat: currentRoute.value.view === 'voice' && !!currentRoute.value.showChat }))
  return { joined: join }
}

// The Talk's chat without joining: the Talk opens as a preview with its chat.
function handleOpenTalkChat(channel: Channel) {
  closeNav()
  navigate(talkPath(channel, { chat: true }))
}

function handleTextClick(channel: Channel) {
  chatStore.selectChannel(channel)
  voiceStore.activeView = 'chat'
  navigate(textPath(channel))
}

const { closeNav } = useCompactLayout()

function handleChannelClick(channel: Channel) {
  // Also when the channel is already open: the drawer has done its job.
  closeNav()
  if (channel.type === 'voice') handleVoiceClick(channel)
  else handleTextClick(channel)
}

async function handleVoiceUserClick(channel: Channel, user: VoiceUser) {
  if (voiceStore.mediaState[user.id]?.screen) {
    if (voiceStore.currentChannelId !== channel.id) {
      const started = await handleVoiceClick(channel)
      if (!started) return
      // A refused join (no permission, media that cannot be sealed) leaves the member outside the Talk, and no screen to watch.
      await started.joined.catch(() => {})
      if (voiceStore.currentChannelId !== channel.id) return
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
  enabled: () => permissions.can('manage_channels'),
  // Touch: holding an item and letting go without moving opens its menu.
  onLongPress: (_kind: 'channel' | 'category', entity: Channel | Category, e: PointerEvent) => ('channels' in entity ? openCategoryMenu(e, entity) : openChannelMenu(e, entity))
})

// Channel rows accept boundary drops; inside drops belong to categories.
function channelIndicator(id: string): '' | 'top' | 'bottom' {
  const indicator = indicatorFor(`channel:${id}`)
  return indicator === 'inside' ? '' : indicator
}

</script>

<template>
  <aside class="w-full min-w-0 bg-mnema-raised flex flex-col h-full select-none">
    <SidebarServerMenu
      @open-admin="emit('open-admin')"
      @open-legal="emit('open-legal')"
      @create-channel="openCreateChannel('text')"
      @create-category="openCreateCategory()"
      @mark-all-read="markEverythingRead"
    />

    <!-- Navigation Scroll Area -->
    <nav
      ref="navEl"
      :class="['flex-1 min-h-0 flex flex-col overflow-y-auto overflow-x-hidden px-2 pt-3', permissions.can('manage_channels') && '[-webkit-touch-callout:none]']"
      :aria-label="$t('sidebar.channels')"
      v-on="navListeners"
      @contextmenu="openListMenu"
    >
      <SidebarDmSection />
      <SidebarEventsEntry />
      <div class="shrink-0">
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
            :admin="permissions.can('manage_channels')"
            :dragging="isDragged('category', category.id)"
            :indicator="indicatorFor(`header:${category.id}`)"
            :flash="flashKey === `category:${category.id}`"
            :hint-id="hintId"
            :unread-channels="activityOf(category).unreadChannels"
            :mentions="activityOf(category).mentions"
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
              :admin="permissions.can('manage_channels')"
              :dragging="isDragged('channel', channel.id)"
              :indicator="channelIndicator(channel.id)"
              :flash="flashKey === `channel:${channel.id}`"
              :hint-id="hintId"
              @open="handleChannelClick(channel)"
              @open-chat="handleOpenTalkChat(channel)"
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
    <p v-if="permissions.can('manage_channels')" :id="hintId" class="sr-only">{{ $t('sidebar.reorderHint') }}</p>
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

    <ChannelWebhooksDialog v-if="webhooksFor" :channel="webhooksFor" @close="webhooksFor = null" />
    <ChannelPermissionsDialog
      v-if="permissionsFor"
      :kind="permissionsFor.kind"
      :target="permissionsFor.entity"
      @close="permissionsFor = null"
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
