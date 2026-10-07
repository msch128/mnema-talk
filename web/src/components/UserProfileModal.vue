<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { ref, computed } from 'vue'
import { 
  X, Crown, Shield, User, Calendar, Volume2,
  Camera, AtSign, Loader2, Edit3, Save, Eraser
} from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import BaseDialog from './BaseDialog.vue'
import PresenceDot from './PresenceDot.vue'
import ActivityStats from './ActivityStats.vue'
import VoiceTimer from './VoiceTimer.vue'

const props = defineProps({
  user: {
    type: Object as PropType<Partial<ProfileUser> | null>,
    default: null
  }
})

const emit = defineEmits<{ close: []; mention: [username: string] }>()

const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const toasts = useToastStore()

const fileInput = ref<HTMLInputElement | null>(null)
const isUploading = ref(false)

// Bio editing state
const isEditingBio = ref(false)
const editDisplayName = ref('')
const editBio = ref('')
const isSavingProfile = ref(false)
const profileSaveError = ref('')

// Determine the active user object
const profileUser = computed<Partial<ProfileUser>>(() => {
  return props.user || chatStore.selectedUserProfile || authStore.user || {}
})

// The profile opens with a stub (no bio) that fills in once /api/users/:id
// answers, so the edit fields are copied when editing starts. Your own
// account record is complete, so it wins; without a loaded bio there is
// nothing safe to send (the PUT replaces both fields).
const editSource = computed(() => {
  const own = authStore.user
  if (isSelf.value && own && 'bio' in own) return own
  const p = profileUser.value
  return 'bio' in p ? p : null
})
const canEditProfile = computed(() => isSelf.value && !!editSource.value)

function startEditBio() {
  const src = editSource.value
  if (!src) return
  editDisplayName.value = src.display_name || ''
  editBio.value = src.bio || ''
  profileSaveError.value = ''
  isEditingBio.value = true
}

function cancelEditBio() {
  isEditingBio.value = false
  profileSaveError.value = ''
  editDisplayName.value = ''
  editBio.value = ''
}

async function saveProfile() {
  if (!canEditProfile.value) return
  isSavingProfile.value = true
  profileSaveError.value = ''
  try {
    const updated = await authStore.updateProfile({
      displayName: editDisplayName.value,
      bio: editBio.value
    })
    if (chatStore.selectedUserProfile) {
      chatStore.selectedUserProfile = { ...chatStore.selectedUserProfile, ...updated }
    }
    isEditingBio.value = false
    toasts.success(t('profile.profileSaved'))
  } catch (err) {
    profileSaveError.value = caughtErrorMessage(err, t('profile.saveFailed'))
  } finally {
    isSavingProfile.value = false
  }
}

// Password change state (own profile only)
const isChangingPassword = ref(false)
const currentPassword = ref('')
const newPassword = ref('')
const newPasswordRepeat = ref('')
const isSavingPassword = ref(false)
const passwordError = ref('')

async function savePassword() {
  passwordError.value = ''
  if ([...newPassword.value].length < MIN_PASSWORD_LENGTH) {
    passwordError.value = t('profile.passwordTooShort', { count: MIN_PASSWORD_LENGTH })
    return
  }
  if (newPassword.value !== newPasswordRepeat.value) {
    passwordError.value = t('profile.passwordMismatch')
    return
  }
  isSavingPassword.value = true
  try {
    await authStore.changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = newPassword.value = newPasswordRepeat.value = ''
    isChangingPassword.value = false
    toasts.success(t('profile.passwordChanged'))
  } catch (err) {
    passwordError.value = caughtErrorMessage(err, t('profile.saveFailed'))
  } finally {
    isSavingPassword.value = false
  }
}

const isSelf = computed(() => {
  return authStore.user && profileUser.value.id === authStore.user.id
})

const liveStatus = computed(() => {
  const live = chatStore.presenceOf(profileUser.value.id ?? null)
  // Your own dot shows your choice even before the first snapshot arrives.
  if (live === 'offline' && isSelf.value) return authStore.user?.presence || 'online'
  return live
})

// Status line: the user edits their own; an admin may edit or clear anyone's.
const MAX_STATUS = 32
const canEditStatus = computed(() => isSelf.value || authStore.isAdmin)
const isEditingStatus = ref(false)
const editStatus = ref('')
const isSavingStatus = ref(false)

function startEditStatus() {
  editStatus.value = profileUser.value.status_text || ''
  isEditingStatus.value = true
}

async function saveStatus(text = editStatus.value) {
  const id = profileUser.value.id
  if (!id) return
  isSavingStatus.value = true
  try {
    const updated = await chatStore.setStatusText(id, text.trim())
    chatStore.selectedUserProfile = chatStore.selectedUserProfile && { ...chatStore.selectedUserProfile, status_text: updated.status_text }
    isEditingStatus.value = false
    toasts.success(text.trim() ? t('profile.statusSaved') : t('profile.statusCleared'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.saveFailed')))
  } finally {
    isSavingStatus.value = false
  }
}

// Current voice hangout channel of this user
const voiceHangout = computed(() => {
  if (!profileUser.value.id) return null
  for (const [chId, users] of Object.entries(voiceStore.channelUsers || {})) {
    if (users && users[profileUser.value.id]) {
      const allChannels = [
        ...chatStore.categories.flatMap(c => c.channels || []),
        ...chatStore.uncategorized
      ]
      return allChannels.find(c => c.id === chId) || { id: chId, name: t('profile.talk') }
    }
  }
  return null
})

// Formatted join date
const memberSinceFormatted = computed(() => {
  const dateStr = profileUser.value.created_at
  if (!dateStr) return t('profile.unknown')
  try {
    const d = new Date(dateStr)
    return new Intl.DateTimeFormat(locale.value, {
      day: 'numeric',
      month: 'long',
      year: 'numeric'
    }).format(d)
  } catch {
    return dateStr
  }
})

function triggerAvatarUpload() {
  if (fileInput.value) {
    fileInput.value.click()
  }
}

async function onAvatarSelected(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return

  // Validate size (max 10MB)
  if (file.size > 10 * 1024 * 1024) {
    toasts.error(t('profile.avatarTooBig', { size: 10 }))
    return
  }

  isUploading.value = true

  try {
    const updated = await authStore.uploadAvatar(file)
    // Update profile view immediately
    if (chatStore.selectedUserProfile) {
      chatStore.selectedUserProfile = { ...chatStore.selectedUserProfile, ...updated }
    }
    toasts.success(t('profile.avatarSaved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.avatarFailed')))
  } finally {
    isUploading.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}

function handleMention() {
  const username = profileUser.value.username
  if (!username) return
  emit('mention', username)
  emit('close')
}
</script>

<template>
  <BaseDialog panel-class="max-w-sm select-none bg-mnema-surface" @close="emit('close')">
    <template #default="{ titleId }">
    <div class="flex min-h-0 flex-col overflow-y-auto">
      <!-- Top banner -->
      <div class="h-28 bg-gradient-to-br from-emerald-950 via-mnema-raised to-emerald-900 border-b border-mnema-hairline relative flex items-start justify-end p-3 overflow-hidden">
        <!-- Background pattern -->
        <div class="absolute inset-0 opacity-20 bg-[radial-gradient(#10b981_1px,transparent_1px)] [background-size:12px_12px] pointer-events-none"></div>

        <button 
          @click="emit('close')"
          data-dialog-close
          class="relative z-10 w-7 h-7 rounded-full bg-black/50 hover:bg-black/80 text-mnema-muted hover:text-mnema-text flex items-center justify-center transition border border-white/10"
          v-tooltip="{ text: $t('common.close'), shortcut: 'Esc' }"
        >
          <X class="w-4 h-4" />
        </button>
      </div>

      <!-- Profile Header (Avatar overlap & badges) -->
      <div class="px-5 pb-5 pt-0 relative bg-mnema-surface">
        <div class="flex justify-between items-end -mt-12 mb-3">
          <!-- Large Avatar with overlap -->
          <div class="relative group">
            <div 
              class="w-20 h-20 rounded-full border-4 border-mnema-surface shadow-xl bg-mnema-canvas overflow-hidden flex items-center justify-center relative cursor-pointer"
              @click="isSelf ? triggerAvatarUpload() : null"
            >
              <img 
                v-if="profileUser.avatar_url" 
                :src="profileUser.avatar_url" 
                :alt="profileUser.display_name || $t('user.avatar')"
                class="w-full h-full object-cover"
              />
              <span v-else class="text-2xl font-bold text-mnema-mint">
                {{ (profileUser.display_name || profileUser.username || '?').charAt(0).toUpperCase() }}
              </span>

              <!-- Hover overlay for own profile -->
              <div 
                v-if="isSelf"
                class="absolute inset-0 bg-black/60 opacity-0 group-hover:opacity-100 flex flex-col items-center justify-center text-white transition-opacity duration-150"
              >
                <Camera class="w-5 h-5 mb-0.5" />
                <span class="text-xs font-bold tracking-wider uppercase">{{ $t('profile.change') }}</span>
              </div>

              <!-- Uploading Spinner -->
              <div 
                v-if="isUploading"
                class="absolute inset-0 bg-black/75 flex flex-col items-center justify-center text-mnema-accent"
              >
                <Loader2 class="w-6 h-6 animate-spin" />
              </div>
            </div>

            <!-- Live status -->
            <span
              class="absolute bottom-0.5 right-0.5"
              v-tooltip.visual="$t(`presence.${liveStatus}`)"
              role="img"
              :aria-label="$t(`presence.${liveStatus}`)"
            >
              <PresenceDot :status="liveStatus" :size="14" ring-class="bg-mnema-surface" />
            </span>

            <!-- Hidden File Input for Avatar Upload -->
            <input 
              v-if="isSelf"
              ref="fileInput"
              type="file"
              accept="image/png,image/jpeg,image/webp,image/gif"
              class="hidden"
              @change="onAvatarSelected"
            />
          </div>

          <!-- Action Buttons in Top Right -->
          <div class="flex items-center gap-2">
            <button 
              v-if="!isSelf"
              @click="handleMention"
              class="px-3 py-1.5 rounded-lg bg-mnema-band hover:bg-mnema-raised text-mnema-text text-sm font-medium border border-mnema-border flex items-center gap-1.5 transition active:scale-95 shadow-sm"
              v-tooltip.visual="$t('profile.mentionHint')"
            >
              <AtSign class="w-4 h-4 text-mnema-accent" />
              <span>{{ $t('profile.mention') }}</span>
            </button>

            <button 
              v-if="isSelf"
              @click="triggerAvatarUpload"
              :disabled="isUploading"
              class="px-3 py-1.5 rounded-lg bg-mnema-accent/15 hover:bg-mnema-accent/25 text-mnema-accent text-sm font-medium border border-mnema-accent/30 flex items-center gap-1.5 transition active:scale-95 disabled:opacity-50"
            >
              <Camera class="w-4 h-4" />
              <span>{{ $t('profile.changeAvatar') }}</span>
            </button>
          </div>
        </div>

        <!-- Name & Badges -->
        <div class="bg-mnema-canvas/70 border border-mnema-hairline rounded-xl p-3.5 shadow-inner">
          <div class="flex items-center justify-between">
            <div>
              <h2 :id="titleId" class="text-lg font-bold text-mnema-text leading-tight flex items-center gap-1.5">
                {{ profileUser.display_name || profileUser.username }}
                <Crown 
                  v-if="profileUser.role === 'admin'" 
                  class="w-4 h-4 text-amber-400 inline-block drop-shadow-[0_0_8px_rgba(251,191,36,0.5)]" 
                  v-tooltip.visual="$t('profile.adminHint')"
                  role="img"
                  :aria-label="$t('profile.adminHint')"
                />
              </h2>
              <p class="text-sm text-mnema-tertiary font-mono">@{{ profileUser.username }}</p>
              <p class="mt-0.5 flex items-center gap-1.5 text-xs text-mnema-tertiary">
                <PresenceDot :status="liveStatus" :size="8" :ring="false" />
                <span data-testid="profile-presence">{{ $t(`presence.${liveStatus}`) }}</span>
              </p>
            </div>

            <!-- Role Badge -->
            <div 
              :class="[
                'px-2 py-0.5 rounded-md text-xs font-semibold border flex items-center gap-1 uppercase tracking-wider',
                profileUser.role === 'admin'
                  ? 'bg-amber-500/10 text-amber-400 border-amber-500/30'
                  : 'bg-mnema-band text-mnema-muted border-mnema-border'
              ]"
            >
              <Shield v-if="profileUser.role === 'admin'" class="w-3.5 h-3.5" />
              <User v-else class="w-3.5 h-3.5" />
              <span>{{ profileUser.role === 'admin' ? $t('role.admin') : $t('role.member') }}</span>
            </div>
          </div>

          <!-- Status line -->
          <div v-if="profileUser.status_text || canEditStatus || isEditingStatus" class="mt-3" data-testid="profile-status">
            <div class="mb-1 flex items-center justify-between">
              <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('profile.status') }}</span>
              <div v-if="canEditStatus && !isEditingStatus" class="flex items-center gap-2">
                <button
                  v-if="!isSelf && profileUser.status_text"
                  type="button"
                  data-testid="profile-status-clear"
                  class="flex items-center gap-1 text-xs text-mnema-danger hover:underline"
                  :disabled="isSavingStatus"
                  @click="saveStatus('')"
                >
                  <Eraser class="h-3.5 w-3.5" />
                  <span>{{ $t('profile.clearStatus') }}</span>
                </button>
                <button
                  type="button"
                  data-testid="profile-status-edit"
                  class="flex items-center gap-1 text-xs text-mnema-accent hover:underline"
                  @click="startEditStatus"
                >
                  <Edit3 class="h-3.5 w-3.5" />
                  <span>{{ isSelf ? $t('profile.edit') : $t('profile.moderateStatus') }}</span>
                </button>
              </div>
            </div>
            <form v-if="isEditingStatus" class="flex items-center gap-1.5" @submit.prevent="saveStatus()">
              <input
                v-model="editStatus"
                data-testid="profile-status-input"
                type="text"
                :maxlength="MAX_STATUS"
                :aria-label="$t('profile.status')"
                :placeholder="$t('profile.statusPlaceholder')"
                class="min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-none"
                @keydown.esc.stop.prevent="isEditingStatus = false"
              />
              <span class="w-10 text-right text-xs tabular-nums text-mnema-tertiary">{{ editStatus.length }}/{{ MAX_STATUS }}</span>
              <button
                type="submit"
                :disabled="isSavingStatus"
                class="flex items-center gap-1 rounded bg-mnema-accent px-2.5 py-1 text-xs font-bold text-mnema-canvas hover:brightness-110 active:scale-95 disabled:opacity-50"
              >
                <Loader2 v-if="isSavingStatus" class="h-3.5 w-3.5 animate-spin" />
                <Save v-else class="h-3.5 w-3.5" />
                <span>{{ $t('common.save') }}</span>
              </button>
            </form>
            <p v-else-if="profileUser.status_text" class="break-words text-sm text-mnema-text">{{ profileUser.status_text }}</p>
            <p v-else class="text-sm italic text-mnema-tertiary">{{ $t('profile.noStatus') }}</p>
          </div>

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <!-- About me (bio) -->
          <div class="mb-3">
            <div class="flex items-center justify-between mb-1">
              <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('profile.about') }}</span>
              <button 
                v-if="canEditProfile && !isEditingBio"
                type="button"
                data-testid="profile-bio-edit"
                @click="startEditBio"
                class="text-xs text-mnema-accent hover:underline flex items-center gap-1"
              >
                <Edit3 class="w-3.5 h-3.5" />
                <span>{{ $t('profile.edit') }}</span>
              </button>
            </div>

            <!-- View Mode -->
            <div v-if="!isEditingBio">
              <p v-if="profileUser.bio" class="text-sm text-mnema-text leading-relaxed whitespace-pre-wrap bg-mnema-band/40 p-2.5 rounded-lg border border-mnema-hairline">
                {{ profileUser.bio }}
              </p>
              <p v-else class="text-sm text-mnema-tertiary italic bg-mnema-band/20 p-2.5 rounded-lg border border-mnema-hairline">
                {{ $t('profile.noBio') }}
              </p>
            </div>

            <!-- Edit Mode (Own Profile) -->
            <div v-else class="space-y-2 mt-1">
              <div>
                <label for="profile-displayname" class="text-xs text-mnema-tertiary block mb-0.5">{{ $t('profile.displayName') }}</label>
                <p class="mb-1 text-xs text-mnema-tertiary">{{ $t('profile.displayNameHint', { username: profileUser.username, count: 24 }) }}</p>
                <input 
                  id="profile-displayname"
                  v-model="editDisplayName" 
                  type="text" 
                  maxlength="24"
                  class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
                />
              </div>
              <div>
                <label for="profile-bio" class="text-xs text-mnema-tertiary block mb-0.5">{{ $t('profile.bioLabel', { count: 250 }) }}</label>
                <textarea 
                  id="profile-bio"
                  v-model="editBio" 
                  rows="3" 
                  maxlength="250"
                  class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent resize-none"
                  :placeholder="$t('profile.bioPlaceholder')"
                ></textarea>
                <div class="flex justify-between items-center text-xs text-mnema-tertiary mt-0.5">
                  <span>{{ editBio.length }} / 250</span>
                  <div class="flex items-center gap-1.5">
                    <button
                      type="button"
                      @click="cancelEditBio"
                      class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text"
                    >
                      {{ $t('common.cancel') }}
                    </button>
                    <button 
                      @click="saveProfile" 
                      :disabled="isSavingProfile"
                      class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50"
                    >
                      <Loader2 v-if="isSavingProfile" class="w-3.5 h-3.5 animate-spin" />
                      <Save v-else class="w-3.5 h-3.5" />
                      <span>{{ $t('common.save') }}</span>
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="profileSaveError" role="alert" class="mt-2 p-1.5 rounded bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-1.5">
              <span>{{ profileSaveError }}</span>
            </div>
          </div>

          <!-- Password (own profile) -->
          <div v-if="isSelf" class="mb-3">
            <div class="flex items-center justify-between mb-1">
              <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('profile.password') }}</span>
              <button
                v-if="!isChangingPassword"
                @click="isChangingPassword = true; passwordError = ''"
                class="text-xs text-mnema-accent hover:underline flex items-center gap-1"
              >
                <Edit3 class="w-3.5 h-3.5" />
                <span>{{ $t('profile.changePassword') }}</span>
              </button>
            </div>

            <form v-if="isChangingPassword" class="space-y-2 mt-1" @submit.prevent="savePassword">
              <input
                v-model="currentPassword"
                type="password"
                autocomplete="current-password"
                :placeholder="$t('profile.currentPassword')" :aria-label="$t('profile.currentPassword')"
                class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
              />
              <input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                :placeholder="$t('profile.newPassword', { count: MIN_PASSWORD_LENGTH })" :aria-label="$t('profile.newPassword', { count: MIN_PASSWORD_LENGTH })"
                class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
              />
              <input
                v-model="newPasswordRepeat"
                type="password"
                autocomplete="new-password"
                :placeholder="$t('profile.repeatPassword')" :aria-label="$t('profile.repeatPassword')"
                class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
              />
              <div class="flex justify-end items-center gap-1.5 text-xs">
                <button
                  type="button"
                  @click="isChangingPassword = false; currentPassword = newPassword = newPasswordRepeat = ''"
                  class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text"
                >
                  {{ $t('common.cancel') }}
                </button>
                <button
                  type="submit"
                  :disabled="isSavingPassword"
                  class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50"
                >
                  <Loader2 v-if="isSavingPassword" class="w-3.5 h-3.5 animate-spin" />
                  <Save v-else class="w-3.5 h-3.5" />
                  <span>{{ $t('common.save') }}</span>
                </button>
              </div>
            </form>

            <div v-if="passwordError" role="alert" class="mt-2 p-1.5 rounded bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-1.5">
              <span>{{ passwordError }}</span>
            </div>
          </div>

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <!-- Voice activity -->
          <div class="mb-3">
            <div class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary mb-1">{{ $t('profile.activity') }}</div>
            <div 
              v-if="voiceHangout" 
              class="flex items-center gap-2 p-2 rounded-lg bg-mnema-accent/10 border border-mnema-accent/25 text-mnema-accent text-sm"
            >
              <Volume2 class="w-4 h-4 flex-shrink-0" />
              <div class="truncate">
                <span class="font-medium">{{ $t('profile.inVoice') }}</span>
                <span class="font-bold ml-1 text-mnema-text">#{{ voiceHangout.name }}</span>
              </div>
              <VoiceTimer :since="voiceStore.joinedAtOf(profileUser.id ?? null)" class="ml-auto text-xs" />
            </div>
            <div 
              v-else 
              class="flex items-center gap-2 p-2 rounded-lg bg-mnema-raised/60 border border-mnema-hairline text-mnema-muted text-sm"
            >
              <span class="w-2 h-2 rounded-full bg-mnema-tertiary"></span>
              <span>{{ $t('profile.notInVoice') }}</span>
            </div>
          </div>

          <!-- Totals: time in Talks and messages sent -->
          <div class="mb-3">
            <div class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary mb-1">{{ $t('activity.title') }}</div>
            <ActivityStats :user="profileUser" variant="full" />
          </div>

          <!-- Membership -->
          <div>
            <div class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary mb-1">{{ $t('profile.membership') }}</div>
            <div class="flex items-center gap-2 text-sm text-mnema-muted">
              <Calendar class="w-4 h-4 text-mnema-tertiary" />
              <span>{{ $t('profile.memberSince') }} <strong class="text-mnema-text font-medium">{{ memberSinceFormatted }}</strong></span>
            </div>
          </div>
        </div>

        <!-- Hint -->
        <div v-if="isSelf" class="mt-3 text-center">
          <p class="text-xs text-mnema-tertiary">
            {{ $t('profile.avatarHint') }}
          </p>
        </div>
      </div>
    </div>
    </template>
  </BaseDialog>
</template>
