<script setup>
import { ref, computed, onMounted } from 'vue'
import {
  Users,
  FolderTree,
  Link,
  HardDrive,
  RefreshCw,
  Search,
  Trash2,
  Edit2,
  ArrowUp,
  ArrowDown,
  KeyRound,
  UserX,
  UserCheck,
  PhoneOff,
  LogOut,
  Plus,
  Save,
  Check,
  Copy,
  Hash,
  Volume2,
  GripVertical
} from '@lucide/vue'
import { api } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t, locale } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UserAvatar from './UserAvatar.vue'

const emit = defineEmits(['close'])
const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const toasts = useToastStore()

const TABS = ['users', 'channels', 'invites', 'media']
const activeTab = ref('users')

// Users tab state
const users = ref([])
const userSearch = ref('')
const passwordModalUser = ref(null)
const newPasswordInput = ref('')
const isResettingPassword = ref(false)

// Channels tab state
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

// Invites & Storage/Media state
const stats = ref({ total_files: 0, total_size_bytes: 0, deleted_files: 0 })
const mediaItems = ref([])
const invites = ref([])
const newInviteUses = ref('')
const newInviteHours = ref('')
const copiedCode = ref('')
const pruneDays = ref('')
const isPruning = ref(false)

function showError(e) {
  toasts.error(e?.message || t('admin.unknownError'))
}

// ---- Data fetching ----

async function loadChannelsData() {
  try {
    const data = await api('/api/channels')
    layoutCategories.value = (data?.categories || []).map(cat => ({
      ...cat,
      channels: (cat.channels || []).map(ch => ({ ...ch }))
    }))
    layoutUncategorized.value = (data?.uncategorized || []).map(ch => ({ ...ch }))
    hasLayoutChanges.value = false
  } catch (e) {
    showError(e)
  }
}

async function refreshUsers() {
  try {
    const u = await api('/api/admin/users')
    users.value = u || []
  } catch (e) {
    showError(e)
  }
}

async function refresh() {
  try {
    const [s, m, i, u] = await Promise.all([
      api('/api/admin/media/stats').catch(() => ({ total_files: 0, total_size_bytes: 0, deleted_files: 0 })),
      api('/api/admin/media?limit=50').catch(() => []),
      api('/api/admin/invites').catch(() => []),
      api('/api/admin/users').catch(() => [])
    ])
    stats.value = s || { total_files: 0, total_size_bytes: 0, deleted_files: 0 }
    mediaItems.value = m || []
    invites.value = i || []
    users.value = u || []
    await loadChannelsData()
  } catch (e) {
    showError(e)
  }
}

// ---- Users management ----

const filteredUsers = computed(() => {
  const q = userSearch.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter(u =>
    (u.username && u.username.toLowerCase().includes(q)) ||
    (u.display_name && u.display_name.toLowerCase().includes(q))
  )
})

function isUserInVoice(userId) {
  if (!voiceStore.channelUsers) return false
  for (const group of Object.values(voiceStore.channelUsers)) {
    if (group && group[userId]) return true
  }
  return false
}

function userVoiceChannelName(userId) {
  if (!voiceStore.channelUsers) return ''
  for (const [chId, group] of Object.entries(voiceStore.channelUsers)) {
    if (group && group[userId]) {
      const ch = chatStore.allChannels?.find(c => c.id === chId)
      return ch?.name || chId
    }
  }
  return ''
}

function formatDateTime(dt) {
  if (!dt) return t('admin.never')
  try {
    return new Date(dt).toLocaleString(locale.value)
  } catch {
    return String(dt)
  }
}

async function toggleDisableUser(u) {
  const isDisabled = !!(u.disabled || u.is_disabled)
  if (!isDisabled) {
    const ok = await confirm({
      title: t('admin.disableUserTitle', { name: u.display_name || u.username }),
      body: t('admin.disableUserBody'),
      confirmLabel: t('admin.disableUserConfirm'),
      danger: true
    })
    if (!ok) return
    try {
      await api(`/api/admin/users/${u.id}`, {
        method: 'PATCH',
        json: { is_disabled: true }
      })
      toasts.success(t('admin.userDisabledToast'))
      await refreshUsers()
    } catch (e) {
      showError(e)
    }
  } else {
    try {
      await api(`/api/admin/users/${u.id}`, {
        method: 'PATCH',
        json: { is_disabled: false }
      })
      toasts.success(t('admin.userEnabledToast'))
      await refreshUsers()
    } catch (e) {
      showError(e)
    }
  }
}

function openPasswordModal(u) {
  passwordModalUser.value = u
  newPasswordInput.value = ''
}

function closePasswordModal() {
  passwordModalUser.value = null
  newPasswordInput.value = ''
}

async function submitPasswordReset() {
  if (!passwordModalUser.value || newPasswordInput.value.length < 8) return
  isResettingPassword.value = true
  try {
    await api(`/api/admin/users/${passwordModalUser.value.id}/password`, {
      method: 'POST',
      json: { new_password: newPasswordInput.value }
    })
    toasts.success(t('admin.passwordResetSuccess'))
    closePasswordModal()
  } catch (e) {
    showError(e)
  } finally {
    isResettingPassword.value = false
  }
}

async function revokeSessions(u) {
  const ok = await confirm({
    title: t('admin.revokeSessionsTitle', { name: u.display_name || u.username }),
    body: t('admin.revokeSessionsBody'),
    confirmLabel: t('admin.revokeSessionsConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/users/${u.id}/sessions`, { method: 'POST' })
    toasts.success(t('admin.sessionsRevoked'))
  } catch (e) {
    showError(e)
  }
}

async function kickUserFromVoice(u) {
  const ok = await confirm({
    title: t('admin.kickFromVoiceTitle', { name: u.display_name || u.username }),
    body: t('admin.kickFromVoiceBody'),
    confirmLabel: t('admin.kickConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/users/${u.id}/kick`, { method: 'POST' })
    toasts.success(t('admin.userKicked'))
  } catch (e) {
    showError(e)
  }
}

// ---- Channels & layout management ----

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

function onCategoryDragStart(e, index) {
  draggedCategoryIndex.value = index
  if (e?.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
  }
}

function onCategoryDrop(e, targetIndex) {
  if (draggedCategoryIndex.value === null || draggedCategoryIndex.value === targetIndex) return
  const item = layoutCategories.value.splice(draggedCategoryIndex.value, 1)[0]
  layoutCategories.value.splice(targetIndex, 0, item)
  draggedCategoryIndex.value = null
  hasLayoutChanges.value = true
}

function onChannelDragStart(e, categoryId, index) {
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
  try {
    await api(`/api/admin/categories/${editingCategory.value.id}`, {
      method: 'PATCH',
      json: { name: editCategoryName.value.trim() }
    })
    toasts.success(t('admin.categoryUpdated'))
    closeEditCategory()
    await chatStore.fetchChannels()
    await loadChannelsData()
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
  try {
    await api(`/api/admin/channels/${editingChannel.value.id}`, {
      method: 'PATCH',
      json: {
        name: editChannelName.value.trim(),
        topic: editChannelTopic.value.trim()
      }
    })
    toasts.success(t('admin.channelUpdated'))
    closeEditChannel()
    await chatStore.fetchChannels()
    await loadChannelsData()
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
    await chatStore.fetchChannels()
    await loadChannelsData()
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
    await chatStore.fetchChannels()
    await loadChannelsData()
  } catch (e) {
    showError(e)
  }
}

async function createCategory() {
  if (!newCategoryName.value.trim()) return
  try {
    await api('/api/admin/categories', {
      method: 'POST',
      json: { name: newCategoryName.value.trim(), sort_order: layoutCategories.value.length }
    })
    newCategoryName.value = ''
    toasts.success(t('admin.categoryUpdated'))
    await chatStore.fetchChannels()
    await loadChannelsData()
  } catch (e) {
    showError(e)
  }
}

// ---- Invites management ----

async function createInvite() {
  try {
    const body = {}
    if (newInviteUses.value) body.max_uses = parseInt(newInviteUses.value, 10)
    if (newInviteHours.value) body.expires_in_hours = parseInt(newInviteHours.value, 10)
    await api('/api/admin/invites', { method: 'POST', json: body })
    newInviteUses.value = ''
    newInviteHours.value = ''
    invites.value = await api('/api/admin/invites')
    toasts.success(t('admin.inviteCreated'))
  } catch (e) {
    showError(e)
  }
}

async function deleteInvite(inv) {
  const ok = await confirm({
    title: t('admin.deleteInviteTitle', { code: inv.code }),
    body: t('admin.deleteInviteBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/invites/${inv.id}`, { method: 'DELETE' })
    invites.value = invites.value.filter(i => i.id !== inv.id)
    toasts.success(t('admin.inviteDeleted'))
  } catch (e) {
    showError(e)
  }
}

async function copyInviteLink(code) {
  const url = `${window.location.origin}/?invite=${encodeURIComponent(code)}`
  try {
    await navigator.clipboard.writeText(url)
    copiedCode.value = code
    toasts.success(t('admin.linkCopied'))
    setTimeout(() => { copiedCode.value = '' }, 2000)
  } catch {
    toasts.error(t('admin.copyFailed'), { detail: url })
  }
}

function inviteStatus(inv) {
  if (inv.expires_at && new Date(inv.expires_at) < new Date()) return t('admin.expired')
  if (inv.max_uses != null && inv.uses_count >= inv.max_uses) return t('admin.usedUp')
  return inv.expires_at
    ? t('admin.validUntil', { date: new Date(inv.expires_at).toLocaleString(locale.value) })
    : t('admin.unlimited')
}

// ---- Storage & Media management ----

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

async function runPrune() {
  const days = parseInt(pruneDays.value, 10)
  if (!days || days < 1) return
  const ok = await confirm({
    title: t('admin.pruneTitle', { days }),
    body: t('admin.pruneBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  isPruning.value = true
  try {
    const data = await api(`/api/admin/media/prune?days=${days}`, { method: 'POST' })
    toasts.success(t('admin.pruned', { count: data.pruned_count, days }))
    pruneDays.value = ''
    await refresh()
  } catch (e) {
    showError(e)
  } finally {
    isPruning.value = false
  }
}

async function deleteMedia(item) {
  const ok = await confirm({
    title: t('admin.deleteFileTitle', { name: item.original_filename }),
    body: t('admin.deleteFileBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/media/${item.id}`, { method: 'DELETE' })
    toasts.success(t('admin.fileDeleted'))
    await refresh()
  } catch (e) {
    showError(e)
  }
}

onMounted(refresh)
</script>

<template>
  <BaseDialog
    :title="$t('admin.title')"
    :subtitle="$t('admin.signedInAs', { name: authStore.user?.display_name || authStore.user?.username || '' })"
    panel-class="max-w-5xl max-h-[90vh]"
    @close="emit('close')"
  >
    <!-- Navigation tabs -->
    <div class="flex items-center gap-1 px-6 py-2.5 border-b border-mnema-hairline bg-mnema-canvas/40 text-sm overflow-x-auto flex-shrink-0">
      <button
        v-for="tab in TABS"
        :key="tab"
        :data-testid="`tab-${tab}`"
        type="button"
        :aria-pressed="activeTab === tab ? 'true' : 'false'"
        :class="activeTab === tab ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
        class="px-3.5 py-1.5 rounded-md border transition whitespace-nowrap flex items-center gap-2"
        @click="activeTab = tab"
      >
        <Users v-if="tab === 'users'" class="w-4 h-4" />
        <FolderTree v-else-if="tab === 'channels'" class="w-4 h-4" />
        <Link v-else-if="tab === 'invites'" class="w-4 h-4" />
        <HardDrive v-else-if="tab === 'media'" class="w-4 h-4" />
        <span>{{ $t(`admin.tabs.${tab}`) }}</span>
      </button>
    </div>

    <!-- Scrollable tab body -->
    <div class="flex-1 overflow-y-auto p-6 space-y-6">

      <!-- ================= 1. USERS TAB ================= -->
      <section v-if="activeTab === 'users'" class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
              <Users class="w-4 h-4 text-mnema-accent" />
              <span>{{ $t('admin.users') }}</span>
            </h3>
            <span class="text-xs text-mnema-tertiary">({{ users.length }})</span>
          </div>

          <div class="flex items-center gap-2 flex-1 max-w-sm ml-auto">
            <div class="relative flex-1">
              <Search class="w-4 h-4 text-mnema-tertiary absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none" />
              <input
                v-model="userSearch"
                type="search"
                :placeholder="$t('admin.searchUsers')"
                :aria-label="$t('admin.searchUsersLabel')"
                class="w-full bg-mnema-surface border border-mnema-border-field rounded-md pl-8 pr-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
              />
            </div>
            <button
              type="button"
              class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition flex-shrink-0"
              :title="$t('admin.refresh')"
              :aria-label="$t('admin.refresh')"
              @click="refreshUsers"
            >
              <RefreshCw class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- Users Table -->
        <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden">
          <div class="overflow-x-auto">
            <table class="w-full text-left text-sm divide-y divide-mnema-hairline">
              <thead class="bg-mnema-canvas/50 text-xs uppercase font-mono text-mnema-tertiary tracking-wider select-none">
                <tr>
                  <th scope="col" class="py-3 px-4">{{ $t('admin.username') }}</th>
                  <th scope="col" class="py-3 px-4">{{ $t('admin.displayName') }}</th>
                  <th scope="col" class="py-3 px-4">{{ $t('admin.role') }}</th>
                  <th scope="col" class="py-3 px-4">{{ $t('admin.lastSeen') }}</th>
                  <th scope="col" class="py-3 px-4">{{ $t('admin.status') }}</th>
                  <th scope="col" class="py-3 px-4 text-right">{{ $t('admin.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-mnema-hairline">
                <tr v-if="!filteredUsers.length">
                  <td colspan="6" class="p-6 text-center text-mnema-tertiary italic">
                    {{ $t('admin.noUsersFound') }}
                  </td>
                </tr>
                <tr
                  v-for="u in filteredUsers"
                  :key="u.id"
                  data-testid="user-row"
                  class="hover:bg-mnema-elevated/60 transition-colors"
                >
                  <!-- Username with avatar -->
                  <td class="py-3 px-4">
                    <div class="flex items-center gap-2.5 min-w-0">
                      <UserAvatar :user="u" size="xs" />
                      <span data-testid="user-username" class="font-mono text-xs font-semibold text-mnema-text truncate">@{{ u.username }}</span>
                    </div>
                  </td>

                  <!-- Display name -->
                  <td class="py-3 px-4">
                    <span data-testid="user-displayname" class="text-mnema-text font-medium truncate block max-w-[160px]">{{ u.display_name || u.username }}</span>
                  </td>

                  <!-- Role -->
                  <td class="py-3 px-4">
                    <span
                      v-if="u.role === 'admin'"
                      data-testid="user-role"
                      class="px-2 py-0.5 rounded text-xs font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20"
                    >
                      {{ $t('role.admin') }}
                    </span>
                    <span v-else data-testid="user-role" class="text-xs text-mnema-muted font-normal">
                      {{ $t('role.member') }}
                    </span>
                  </td>

                  <!-- Last Seen -->
                  <td class="py-3 px-4 text-xs text-mnema-muted whitespace-nowrap">
                    <span data-testid="user-lastseen">{{ formatDateTime(u.last_seen_at) }}</span>
                  </td>

                  <!-- Status -->
                  <td class="py-3 px-4 whitespace-nowrap">
                    <div class="flex items-center gap-1.5 flex-wrap">
                      <span
                        v-if="u.disabled || u.is_disabled"
                        data-testid="user-status"
                        class="px-2 py-0.5 rounded text-xs font-medium bg-mnema-danger/10 text-mnema-danger border border-mnema-danger/20"
                      >
                        {{ $t('admin.userDisabled') }}
                      </span>
                      <span
                        v-else
                        data-testid="user-status"
                        class="px-2 py-0.5 rounded text-xs font-medium bg-mnema-accent/10 text-mnema-accent border border-mnema-accent/20"
                      >
                        {{ $t('admin.userActive') }}
                      </span>

                      <!-- In Voice indicator -->
                      <span
                        v-if="isUserInVoice(u.id)"
                        class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-mono bg-mnema-band text-mnema-mint border border-mnema-mint/30"
                        :title="$t('admin.inVoice')"
                      >
                        <Volume2 class="w-3 h-3 flex-shrink-0" />
                        <span class="truncate max-w-[80px]">{{ userVoiceChannelName(u.id) }}</span>
                      </span>
                    </div>
                  </td>

                  <!-- Actions -->
                  <td class="py-3 px-4 text-right whitespace-nowrap">
                    <div class="flex items-center justify-end gap-1">
                      <!-- Kick from voice -->
                      <button
                        v-if="isUserInVoice(u.id)"
                        data-testid="action-kick"
                        type="button"
                        class="p-1.5 rounded text-mnema-danger hover:bg-mnema-danger/15 transition"
                        :title="$t('admin.kickFromVoice')"
                        :aria-label="$t('admin.kickFromVoice')"
                        @click="kickUserFromVoice(u)"
                      >
                        <PhoneOff class="w-4 h-4" />
                      </button>

                      <template v-if="u.role !== 'admin'">
                        <!-- Disable / Enable -->
                        <button
                          data-testid="action-disable"
                          type="button"
                          :class="[
                            'p-1.5 rounded transition',
                            (u.disabled || u.is_disabled)
                              ? 'text-mnema-accent hover:bg-mnema-accent/15'
                              : 'text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-hover'
                          ]"
                          :title="(u.disabled || u.is_disabled) ? $t('admin.enableUser') : $t('admin.disableUser')"
                          :aria-label="(u.disabled || u.is_disabled) ? $t('admin.enableUser') : $t('admin.disableUser')"
                          @click="toggleDisableUser(u)"
                        >
                          <UserCheck v-if="u.disabled || u.is_disabled" class="w-4 h-4" />
                          <UserX v-else class="w-4 h-4" />
                        </button>

                        <!-- Reset Password -->
                        <button
                          data-testid="action-reset-password"
                          type="button"
                          class="p-1.5 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition"
                          :title="$t('admin.resetPassword')"
                          :aria-label="$t('admin.resetPassword')"
                          @click="openPasswordModal(u)"
                        >
                          <KeyRound class="w-4 h-4" />
                        </button>

                        <!-- Revoke Sessions -->
                        <button
                          data-testid="action-revoke-sessions"
                          type="button"
                          class="p-1.5 rounded text-mnema-tertiary hover:text-mnema-warning hover:bg-mnema-hover transition"
                          :title="$t('admin.revokeSessions')"
                          :aria-label="$t('admin.revokeSessions')"
                          @click="revokeSessions(u)"
                        >
                          <LogOut class="w-4 h-4" />
                        </button>
                      </template>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <!-- ================= 2. CHANNELS TAB ================= -->
      <section v-else-if="activeTab === 'channels'" class="space-y-4">
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
      </section>

      <!-- ================= 3. INVITES TAB ================= -->
      <section v-else-if="activeTab === 'invites'" class="space-y-4">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
            <Link class="w-4 h-4 text-mnema-accent" />
            <span>{{ $t('admin.invites') }}</span>
          </h3>
          <span class="text-xs text-mnema-tertiary">{{ $t('admin.inviteOnly') }}</span>
        </div>

        <div class="flex flex-wrap gap-2">
          <input
            v-model="newInviteUses"
            type="number"
            min="1"
            :placeholder="$t('admin.maxUses')"
            :aria-label="$t('admin.maxUses')"
            class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none min-w-0 flex-1 basis-56 focus:border-mnema-accent transition"
          />
          <input
            v-model="newInviteHours"
            type="number"
            min="1"
            :placeholder="$t('admin.validHours')"
            :aria-label="$t('admin.validHours')"
            class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none min-w-0 flex-1 basis-56 focus:border-mnema-accent transition"
          />
          <button
            type="button"
            class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-3.5 py-1.5 rounded-md text-sm transition shadow-sm"
            @click="createInvite"
          >
            {{ $t('admin.createInvite') }}
          </button>
        </div>

        <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
          <div v-if="!invites.length" class="p-4 text-sm text-mnema-tertiary">{{ $t('admin.noInvites') }}</div>
          <div v-for="inv in invites" :key="inv.id" class="px-4 py-2.5 flex items-center justify-between gap-3 text-sm">
            <div class="flex items-center gap-3 min-w-0">
              <span class="font-mono bg-mnema-elevated border border-mnema-border px-2 py-0.5 rounded text-mnema-text font-semibold">{{ inv.code }}</span>
              <span class="text-mnema-muted whitespace-nowrap">{{ $t('admin.uses', { used: inv.uses_count, max: inv.max_uses ?? '∞' }) }}</span>
              <span class="text-mnema-tertiary truncate">{{ inviteStatus(inv) }}</span>
            </div>
            <div class="flex items-center gap-3 flex-shrink-0">
              <button
                type="button"
                class="flex items-center gap-1.5 text-mnema-accent hover:text-mnema-accent-hover transition font-medium"
                @click="copyInviteLink(inv.code)"
              >
                <Check v-if="copiedCode === inv.code" class="w-4 h-4" />
                <Copy v-else class="w-4 h-4" />
                <span>{{ copiedCode === inv.code ? $t('admin.copied') : $t('admin.copyLink') }}</span>
              </button>
              <button
                type="button"
                class="text-mnema-tertiary hover:text-mnema-danger transition"
                :title="$t('admin.deleteInvite')"
                :aria-label="$t('admin.deleteInvite')"
                @click="deleteInvite(inv)"
              >
                <Trash2 class="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </section>

      <!-- ================= 4. MEDIA & STORAGE TAB ================= -->
      <section v-else-if="activeTab === 'media'" class="space-y-6">
        <!-- Storage summary & Prune -->
        <div class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div class="space-y-1">
              <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.storage') }}</span>
              <div class="text-xl font-bold text-mnema-text flex items-baseline gap-2">
                <span>{{ formatBytes(stats.total_size_bytes) }}</span>
                <span class="text-sm font-normal text-mnema-muted">({{ $t('admin.storageCounts', { files: stats.total_files, deleted: stats.deleted_files }) }})</span>
              </div>
              <p class="text-sm text-mnema-muted">{{ $t('admin.retentionOff') }}</p>
            </div>
            <button
              type="button"
              class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition"
              :title="$t('admin.refresh')"
              :aria-label="$t('admin.refresh')"
              @click="refresh"
            >
              <RefreshCw class="w-4 h-4" />
            </button>
          </div>

          <div class="flex flex-wrap items-center gap-2 pt-2 border-t border-mnema-hairline">
            <span class="text-sm text-mnema-muted">{{ $t('admin.pruneLead') }}</span>
            <input
              v-model="pruneDays"
              type="number"
              min="1"
              :placeholder="$t('admin.days')"
              :aria-label="$t('admin.pruneDaysLabel')"
              class="bg-mnema-canvas border border-mnema-border-field rounded-md px-2 py-1 text-sm text-mnema-text w-20 outline-none focus:border-mnema-accent"
            />
            <span class="text-sm text-mnema-muted">{{ $t('admin.days') }}</span>
            <button
              type="button"
              :disabled="!pruneDays || pruneDays < 1 || isPruning"
              class="border border-mnema-danger/40 bg-mnema-danger/10 text-mnema-danger hover:bg-mnema-danger hover:text-mnema-accent-ink font-medium px-3 py-1 rounded-md text-sm transition disabled:opacity-40"
              @click="runPrune"
            >
              {{ isPruning ? $t('admin.deleting') : $t('admin.pruneButton') }}
            </button>
          </div>
        </div>

        <!-- Media Grid -->
        <div class="space-y-3">
          <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider">{{ $t('admin.media') }}</h3>
          <div v-if="!mediaItems.length" class="p-6 text-center text-sm text-mnema-tertiary bg-mnema-surface rounded-lg border border-mnema-hairline">
            {{ $t('admin.noMedia') }}
          </div>
          <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
            <div
              v-for="item in mediaItems"
              :key="item.id"
              class="group relative bg-mnema-surface rounded-md border border-mnema-hairline overflow-hidden aspect-square flex flex-col justify-between"
            >
              <div class="w-full h-full absolute inset-0 bg-mnema-canvas flex items-center justify-center overflow-hidden">
                <img
                  v-if="!item.is_deleted && item.mime_type.startsWith('image/') && item.mime_type !== 'image/svg+xml'"
                  :src="item.url"
                  :alt="item.original_filename"
                  class="w-full h-full object-cover group-hover:scale-105 transition duration-200"
                  loading="lazy"
                />
                <div v-else class="text-sm text-mnema-tertiary p-2 text-center break-all font-mono">
                  {{ item.is_deleted ? $t('admin.deletedMarker') : item.original_filename }}
                </div>
              </div>
              <div class="relative z-10 p-2 bg-gradient-to-t from-black/85 via-black/40 to-transparent flex items-end justify-between mt-auto">
                <div class="text-xs text-white truncate max-w-[120px]">
                  <p class="truncate font-medium">{{ item.original_filename }}</p>
                  <p class="text-mnema-muted">{{ formatBytes(item.size_bytes) }} • {{ item.uploader_name }}</p>
                </div>
                <template v-if="!item.is_deleted">
                  <button
                    type="button"
                    class="p-1 rounded bg-mnema-danger/80 hover:bg-mnema-danger text-mnema-accent-ink transition flex-shrink-0"
                    :title="$t('admin.deleteFile')"
                    :aria-label="$t('admin.deleteFile')"
                    @click.stop="deleteMedia(item)"
                  >
                    <Trash2 class="w-4 h-4" />
                  </button>
                </template>
              </div>
            </div>
          </div>
        </div>
      </section>

    </div>

    <!-- ================= SUB-MODALS ================= -->

    <!-- Reset Password Modal -->
    <BaseDialog
      v-if="passwordModalUser"
      :title="$t('admin.resetPasswordTitle', { name: passwordModalUser.display_name || passwordModalUser.username })"
      panel-class="max-w-md"
      @close="closePasswordModal"
    >
      <form @submit.prevent="submitPasswordReset" class="p-6 space-y-4">
        <p class="text-sm text-mnema-muted">{{ $t('admin.resetPasswordPrompt') }}</p>
        <div>
          <label class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
            {{ $t('admin.newPasswordLabel') }}
          </label>
          <input
            v-model="newPasswordInput"
            data-testid="new-password-input"
            type="password"
            :placeholder="$t('admin.newPasswordPlaceholder')"
            :aria-label="$t('admin.newPasswordLabel')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
          <p v-if="newPasswordInput && newPasswordInput.length < 8" class="text-xs text-mnema-danger mt-1">
            {{ $t('admin.passwordTooShort') }}
          </p>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button
            type="button"
            class="px-3.5 py-1.5 rounded-md border border-mnema-border text-sm text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition"
            @click="closePasswordModal"
          >
            {{ $t('common.cancel') }}
          </button>
          <button
            data-testid="save-password-button"
            type="submit"
            :disabled="newPasswordInput.length < 8 || isResettingPassword"
            class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-4 py-1.5 rounded-md text-sm transition disabled:opacity-40"
          >
            {{ $t('admin.savePassword') }}
          </button>
        </div>
      </form>
    </BaseDialog>

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

  </BaseDialog>
</template>
