<script setup lang="ts">
import { nameInitial } from '../lib/initial'
import { MAX_AVATAR_BYTES, MAX_AVATAR_MB } from '../lib/avatarLimit'
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import type { LivePresence } from './presentationTypes'
import { ref } from 'vue'
import { AtSign, Camera, Loader2, MessageCircle, Trash2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import PresenceDot from './PresenceDot.vue'

// The top of a profile: the large avatar (changeable on one's own profile),
// the live status dot and the buttons next to it.
defineProps({
  profileUser: { type: Object as PropType<Partial<ProfileUser>>, required: true },
  isSelf: { type: Boolean, default: false },
  liveStatus: { type: String as PropType<LivePresence>, required: true },
  // Offer "Message": direct messages are on and this member can be written to.
  canMessage: { type: Boolean, default: false }
})
const emit = defineEmits<{ mention: []; message: [] }>()

const authStore = useAuthStore()
const chatStore = useChatStore()
const toasts = useToastStore()
const features = useServerFeaturesStore()

const fileInput = ref<HTMLInputElement | null>(null)
const isUploading = ref(false)

function triggerAvatarUpload() {
  if (fileInput.value) {
    fileInput.value.click()
  }
}

async function onAvatarSelected(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return

  // Validate size (max 5MB, like the server)
  if (file.size > MAX_AVATAR_BYTES) {
    toasts.error(t('profile.avatarTooBig', { size: MAX_AVATAR_MB }))
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

async function removeAvatar() {
  isUploading.value = true
  try {
    const updated = await authStore.removeAvatar()
    if (chatStore.selectedUserProfile) {
      const next = { ...chatStore.selectedUserProfile, ...updated }
      // A removed avatar is absent from the answer; the merge must not keep the old one.
      delete next.avatar_url
      chatStore.selectedUserProfile = next
    }
    toasts.success(t('profile.avatarRemoved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.avatarRemoveFailed')))
  } finally {
    isUploading.value = false
  }
}
</script>

<template>
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
          {{ nameInitial(profileUser.display_name || profileUser.username || '?') }}
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
        @click="emit('mention')"
        class="btn btn-secondary btn-sm max-md:min-h-11"
        v-tooltip.visual="$t('profile.mentionHint')"
      >
        <AtSign class="w-4 h-4 text-mnema-accent" />
        <span>{{ $t('profile.mention') }}</span>
      </button>

      <button
        v-if="canMessage"
        type="button"
        data-testid="profile-message"
        @click="emit('message')"
        class="btn btn-sm max-md:min-h-11 border-mnema-accent/30 bg-mnema-accent/15 text-mnema-accent hover:bg-mnema-accent/25"
        v-tooltip.visual="$t('dm.messageHint')"
      >
        <MessageCircle class="w-4 h-4" />
        <span>{{ $t('dm.message') }}</span>
      </button>

      <button
        v-if="isSelf"
        @click="triggerAvatarUpload"
        :disabled="isUploading"
        class="btn btn-secondary btn-sm max-md:min-h-11"
      >
        <Camera class="w-4 h-4" aria-hidden="true" />
        <span>{{ $t('profile.changeAvatar') }}</span>
      </button>

      <button
        v-if="isSelf && profileUser.avatar_url && features.avatarRemove"
        type="button"
        data-testid="avatar-remove"
        :disabled="isUploading"
        class="btn btn-ghost btn-sm"
        @click="removeAvatar"
      >
        <Trash2 class="w-4 h-4" aria-hidden="true" />
        <span>{{ $t('profile.removeAvatar') }}</span>
      </button>
    </div>
  </div>
</template>
