<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser, Role } from '../types/domain'
import { ref, computed } from 'vue'
import { X, Crown, Shield, User, Calendar, Volume2, KeyRound } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useAccountStore } from '../stores/account'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { roleColorCss } from '../lib/permissions'
import { useDmActions } from '../composables/useDmActions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { activityText } from '../lib/presence'
import { t, locale } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import PresenceDot from './PresenceDot.vue'
import ProfileAvatar from './ProfileAvatar.vue'
import ProfileBannerControls from './ProfileBannerControls.vue'
import ProfileBio from './ProfileBio.vue'
import ProfileStatus from './ProfileStatus.vue'
import BotBadge from './BotBadge.vue'
import { shownName } from '../lib/deletedUser'
import ActivityStats from './ActivityStats.vue'
import VoiceTimer from './VoiceTimer.vue'
import ProfileMemberActions from './ProfileMemberActions.vue'
import ProfileModerationTab from './ProfileModerationTab.vue'

const props = defineProps({
  user: {
    type: Object as PropType<Partial<ProfileUser> | null>,
    default: null
  }
})

const emit = defineEmits<{ close: []; mention: [username: string] }>()
const account = useAccountStore()

// Password and sign-in security live together in "Account & security".
function openSecurity() {
  emit('close')
  account.open()
}

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
// The badge names the member's highest role, not the base role.
const topRole = computed(() => permissions.rolesOf(profileId.value).reduce<Role | null>((top, r) => (!top || r.position > top.position ? r : top), null))
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
// Direct messages (hidden on a server without them).
const features = useServerFeaturesStore()
const dmActions = useDmActions()
const showMessage = computed(() => dmActions.canMessage(profileId.value))
async function handleMessage() {
  if (await dmActions.messageMember(profileId.value)) emit('close')
}
// Moderators with Manage reports get a Moderation tab: the member's history and
// the private notes about them (never shown to the member, never for yourself).
const tab = ref<'profile' | 'moderation'>('profile')
const strikes = ref(0)
const hasModerationTab = computed(() => features.moderationHistory && permissions.can('manage_reports') && !isSelf.value && !isBot.value && !!profileId.value)

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
  <BaseDialog panel-class="max-w-sm select-none bg-mnema-surface" initial-focus="[data-dialog-close]" @close="emit('close')">
    <template #default="{ titleId }">
    <div class="flex min-h-0 flex-col overflow-y-auto">
      <!-- Top banner -->
      <div class="h-28 shrink-0 bg-linear-to-br from-emerald-950 via-mnema-raised to-emerald-900 border-b border-mnema-hairline relative flex items-start justify-end p-3 overflow-hidden">
        <!-- The member's banner, or the default pattern -->
        <img
          v-if="profileUser.banner_url"
          :src="profileUser.banner_url"
          alt=""
          class="absolute inset-0 h-full w-full object-cover pointer-events-none"
          data-testid="profile-banner"
        />
        <div v-else class="absolute inset-0 opacity-20 bg-[radial-gradient(#10b981_1px,transparent_1px)] bg-size-[12px_12px] pointer-events-none"></div>

        <ProfileBannerControls v-if="isSelf && features.profileBanner" :has-banner="!!profileUser.banner_url" />

        <button 
          @click="emit('close')"
          data-dialog-close
          class="relative z-10 w-7 h-7 max-md:w-11 max-md:h-11 rounded-full bg-black/50 hover:bg-black/80 text-mnema-muted hover:text-mnema-text flex items-center justify-center transition border border-white/10"
          v-tooltip="{ text: $t('common.close'), shortcut: 'Esc' }"
        >
          <X class="w-4 h-4" />
        </button>
      </div>

      <!-- Profile Header (Avatar overlap & badges) -->
      <div class="px-5 pb-5 pt-0 relative bg-mnema-surface">
        <ProfileAvatar :profile-user="profileUser" :is-self="!!isSelf" :live-status="liveStatus" :can-message="showMessage" @mention="handleMention" @message="handleMessage" />

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

            <!-- Role badge: the Owner, else the highest role, else plain member -->
            <div
              v-if="!isBot"
              :class="[
                'flex shrink-0 items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs font-semibold',
                profileUser.role === 'admin'
                  ? 'border-amber-500/30 bg-amber-500/10 text-amber-400 uppercase tracking-wider'
                  : 'border-mnema-border bg-mnema-band text-mnema-muted'
              ]"
              data-testid="profile-role-badge"
            >
              <Shield v-if="profileUser.role === 'admin'" class="w-3.5 h-3.5" />
              <span v-else-if="topRole" class="h-2 w-2 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(topRole.color) || 'transparent' }"></span>
              <User v-else class="w-3.5 h-3.5" />
              <span>{{ profileUser.role === 'admin' ? $t('role.admin') : (topRole?.name ?? $t('role.member')) }}</span>
            </div>
          </div>

          <p v-if="isBot && addedBy" class="mt-3 text-xs text-mnema-tertiary" data-testid="profile-added-by">{{ $t('profile.addedBy', { name: addedBy }) }}</p>

          <div v-if="hasModerationTab" role="group" :aria-label="$t('modHistory.tabs')" class="mt-3 flex gap-1" data-testid="profile-tabs">
            <button
              v-for="name in (['profile', 'moderation'] as const)"
              :key="name"
              type="button"
              :aria-pressed="tab === name ? 'true' : 'false'"
              :data-tab="name"
              :class="['flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold transition max-md:min-h-11', tab === name ? 'border-mnema-accent/40 bg-mnema-accent/15 text-mnema-accent' : 'border-transparent text-mnema-muted hover:text-mnema-text']"
              @click="tab = name"
            >
              {{ $t(name === 'profile' ? 'modHistory.tabProfile' : 'modHistory.tabModeration') }}
              <span v-if="name === 'moderation' && strikes > 0" class="rounded-full bg-mnema-danger/20 px-1.5 text-xs text-mnema-danger" data-testid="strike-badge" :aria-label="$t('modHistory.strikeCount', { count: strikes })">{{ strikes }}</span>
            </button>
          </div>

          <ProfileModerationTab v-if="hasModerationTab" v-show="tab === 'moderation'" class="mt-3" :user-id="profileId" @strikes="strikes = $event" />

          <div v-show="tab === 'profile' || !hasModerationTab">
          <ProfileMemberActions v-if="!isSelf" :profile-user="profileUser" :is-bot="isBot" />
          <ProfileStatus :profile-user="profileUser" :is-self="!!isSelf" />

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <ProfileBio :profile-user="profileUser" :is-self="!!isSelf" />

          <div v-if="isSelf" class="mb-3">
            <button type="button" data-testid="profile-password-link" class="flex items-center gap-1 text-xs text-mnema-accent hover:underline" @click="openSecurity">
              <KeyRound class="w-3.5 h-3.5" /><span>{{ $t('profile.passwordLink') }}</span>
            </button>
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
              <Volume2 class="w-4 h-4 shrink-0" />
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
        </div>
      </div>
    </div>
    </template>
  </BaseDialog>
</template>
