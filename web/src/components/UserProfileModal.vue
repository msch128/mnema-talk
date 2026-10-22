<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { ref, computed } from 'vue'
import { X, Crown, Shield, User, Calendar, Volume2, Lock } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { roleColorCss } from '../lib/permissions'
import { activityText } from '../lib/presence'
import { t, locale } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import PresenceDot from './PresenceDot.vue'
import ProfileAvatar from './ProfileAvatar.vue'
import ProfileBio from './ProfileBio.vue'
import ProfilePassword from './ProfilePassword.vue'
import ProfileStatus from './ProfileStatus.vue'
import BotBadge from './BotBadge.vue'
import { shownName } from '../lib/deletedUser'
import ActivityStats from './ActivityStats.vue'
import VoiceTimer from './VoiceTimer.vue'
import MemberRolesDialog from './MemberRolesDialog.vue'
import { moderationOffer, openTimeout, openBan, liftTimeout } from '../lib/moderationActions'

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

// Determine the active user object
const profileUser = computed<Partial<ProfileUser>>(() => {
  return props.user || chatStore.selectedUserProfile || authStore.user || {}
})

const isSelf = computed(() => {
  return authStore.user && profileUser.value.id === authStore.user.id
})

const liveStatus = computed(() => {
  const live = chatStore.presenceOf(profileUser.value.id ?? null)
  // Your own dot shows your choice even before the first snapshot arrives.
  if (live === 'offline' && isSelf.value) return authStore.user?.presence || 'online'
  return live
})

const permissions = usePermissionsStore()
const profileId = computed(() => profileUser.value.id ?? '')
const profileRoles = computed(() => permissions.rolesOf(profileId.value))
// A bot is a member of the list with the built-in Bot role; the profile may
// be opened from a message, so the list is where its bot-ness and creator live.
const profileMember = computed(() => chatStore.members.find(m => m.id === profileId.value))
const isBot = computed(() => profileMember.value?.is_bot === true)
const botActivity = computed(() => {
  const a = isBot.value && liveStatus.value !== 'offline' ? chatStore.activityOfUser(profileId.value) : null
  return a ? activityText(a) : ''
})
const addedBy = computed(() => {
  const id = profileMember.value?.added_by
  const creator = id ? chatStore.members.find(m => m.id === id) : undefined
  return creator ? creator.display_name || creator.username : ''
})
const rolesDialogUser = computed(() => ({ id: profileId.value, username: profileUser.value.username ?? '', display_name: profileUser.value.display_name ?? '', role: profileUser.value.role ?? 'user' }))
const showRolesDialog = ref(false)
// Roles are given by someone with Manage roles and a higher rank; never the Owner's, never one's own.
const canGiveRoles = computed(() => !!profileId.value && !isSelf.value && !isBot.value && profileUser.value.role !== 'admin' && permissions.can('manage_roles') && permissions.outranksMember(profileId.value))
const offer = computed(() => moderationOffer({ id: profileId.value, role: profileUser.value.role, display_name: profileUser.value.display_name, username: profileUser.value.username }))
const moderationSubject = computed(() => ({ id: profileId.value, display_name: profileUser.value.display_name, username: profileUser.value.username }))

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
        <ProfileAvatar :profile-user="profileUser" :is-self="!!isSelf" :live-status="liveStatus" @mention="handleMention" />

        <!-- Name & Badges -->
        <div class="bg-mnema-canvas/70 border border-mnema-hairline rounded-xl p-3.5 shadow-inner">
          <div class="flex items-center justify-between">
            <div>
              <h2 :id="titleId" class="text-lg font-bold text-mnema-text leading-tight flex items-center gap-1.5">
                {{ shownName(profileUser) }}
                <BotBadge v-if="isBot" />
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
              <p v-if="botActivity" class="mt-0.5 truncate text-xs text-mnema-tertiary" data-testid="profile-activity">{{ botActivity }}</p>
            </div>

            <!-- Role Badge -->
            <div 
              v-if="!isBot"
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

          <!-- Roles -->
          <div v-if="profileRoles.length || canGiveRoles" class="mt-3 flex flex-wrap items-center gap-1.5" data-testid="profile-roles">
            <span
              v-for="role in profileRoles"
              :key="role.id"
              class="inline-flex items-center gap-1 rounded-md border border-mnema-border px-1.5 py-0.5 text-xs text-mnema-text"
            >
              <span class="h-2 w-2 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>{{ role.name }}
              <Lock v-if="role.managed" class="h-3 w-3 text-mnema-tertiary" v-tooltip.visual="$t(role.bot_user_id ? 'roles.botRoleHint' : 'roles.managedHint')" role="img" :aria-label="$t(role.bot_user_id ? 'roles.botRoleHint' : 'roles.managedHint')" />
            </span>
            <button
              v-if="canGiveRoles"
              type="button"
              data-testid="profile-edit-roles"
              class="rounded-md border border-dashed border-mnema-border px-1.5 py-0.5 text-xs text-mnema-tertiary transition hover:border-mnema-accent hover:text-mnema-accent"
              @click="showRolesDialog = true"
            >{{ $t('memberRoles.edit') }}</button>
          </div>

          <p v-if="isBot && addedBy" class="mt-3 text-xs text-mnema-tertiary" data-testid="profile-added-by">{{ $t('profile.addedBy', { name: addedBy }) }}</p>

          <!-- Moderation -->
          <div v-if="!isBot && (offer.timeout || offer.ban)" class="mt-3 flex flex-wrap items-center gap-1.5" data-testid="profile-moderation">
            <button
              v-if="offer.timeout"
              type="button"
              data-testid="profile-timeout"
              class="rounded-md border border-mnema-border px-2 py-1 text-xs text-mnema-text transition hover:border-mnema-danger hover:text-mnema-danger"
              @click="openTimeout(moderationSubject)"
            >{{ $t('moderation.timeoutAction') }}</button>
            <button
              v-if="offer.removeTimeout"
              type="button"
              data-testid="profile-untimeout"
              class="rounded-md border border-mnema-border px-2 py-1 text-xs text-mnema-text transition hover:border-mnema-accent hover:text-mnema-accent"
              @click="liftTimeout(moderationSubject)"
            >{{ $t('moderation.removeTimeout') }}</button>
            <button
              v-if="offer.ban"
              type="button"
              data-testid="profile-ban"
              class="rounded-md border border-mnema-border px-2 py-1 text-xs text-mnema-text transition hover:border-mnema-danger hover:text-mnema-danger"
              @click="openBan(moderationSubject)"
            >{{ $t('moderation.banAction') }}</button>
          </div>

          <ProfileStatus :profile-user="profileUser" :is-self="!!isSelf" />

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <ProfileBio :profile-user="profileUser" :is-self="!!isSelf" />

          <ProfilePassword v-if="isSelf" />

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
    <MemberRolesDialog v-if="showRolesDialog" :user="rolesDialogUser" @close="showRolesDialog = false" />
    </template>
  </BaseDialog>
</template>
