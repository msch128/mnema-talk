<script setup lang="ts">
import type { User as ProfileUser } from '../types/domain'
import { ref } from 'vue'
import { ImagePlus, Trash2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// "Change banner" and "Remove banner" on one's own profile, as quiet pills on
// the banner itself. The server crops the picture to 3:1 and strips its metadata.
defineProps<{ hasBanner: boolean }>()

const MAX_MB = 5
const authStore = useAuthStore()
const chatStore = useChatStore()
const toasts = useToastStore()

const pill = 'btn btn-sm border-white/10 bg-black/50 text-mnema-text backdrop-blur-xs hover:bg-black/75 max-md:min-h-11'
const fileInput = ref<HTMLInputElement | null>(null)
const busy = ref(false)

function showOnProfile(updated: Partial<ProfileUser>) {
  const open = chatStore.selectedUserProfile
  if (!open) return
  const next = { ...open, ...updated }
  // A removed banner is absent from the answer; the merge must not keep the old one.
  if (!updated.banner_url) delete next.banner_url
  chatStore.selectedUserProfile = next
}

async function onSelected(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (file.size > MAX_MB * 1024 * 1024) {
    toasts.error(t('profile.bannerTooBig', { size: MAX_MB }))
    input.value = ''
    return
  }
  busy.value = true
  try {
    showOnProfile(await authStore.uploadBanner(file))
    toasts.success(t('profile.bannerSaved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.bannerFailed')))
  } finally {
    busy.value = false
    input.value = ''
  }
}

async function remove() {
  busy.value = true
  try {
    showOnProfile(await authStore.removeBanner())
    toasts.success(t('profile.bannerRemoved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.bannerRemoveFailed')))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="absolute bottom-2.5 left-3 z-10 flex items-center gap-1.5" data-testid="profile-banner-controls">
    <button
      type="button"
      :class="pill"
      :disabled="busy"
      :aria-busy="busy"
      data-testid="banner-change"
      v-tooltip.visual="$t('profile.bannerHint')"
      @click="fileInput?.click()"
    >
      <ImagePlus class="h-4 w-4" aria-hidden="true" />
      <span>{{ $t('profile.changeBanner') }}</span>
    </button>
    <button
      v-if="hasBanner"
      type="button"
      :class="pill"
      :disabled="busy"
      :aria-label="$t('profile.removeBanner')"
      v-tooltip="$t('profile.removeBanner')"
      data-testid="banner-remove"
      @click="remove"
    >
      <Trash2 class="h-4 w-4" aria-hidden="true" />
    </button>
    <input
      ref="fileInput"
      type="file"
      accept="image/png,image/jpeg,image/webp,image/gif"
      class="hidden"
      data-testid="banner-input"
      @change="onSelected"
    />
  </div>
</template>
