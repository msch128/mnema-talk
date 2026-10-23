<script setup lang="ts">
// Welcome flow, profile step: avatar, display name and bio through the same
// store calls as the profile dialog. Every field is optional.
import { ref } from 'vue'
import { Camera, Loader2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

const MAX_AVATAR_BYTES = 10 * 1024 * 1024

const emit = defineEmits<{ done: []; skip: [] }>()
const auth = useAuthStore()
const toasts = useToastStore()

const displayName = ref(auth.user?.display_name ?? '')
const bio = ref(auth.user?.bio ?? '')
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const saving = ref(false)

async function onAvatar(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (file.size > MAX_AVATAR_BYTES) {
    toasts.error(t('profile.avatarTooBig', { size: 10 }))
    return
  }
  uploading.value = true
  try {
    await auth.uploadAvatar(file)
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.avatarFailed')))
  } finally {
    uploading.value = false
  }
}

async function save() {
  const user = auth.user
  if (user && (displayName.value !== user.display_name || bio.value !== user.bio)) {
    saving.value = true
    try {
      await auth.updateProfile({ displayName: displayName.value, bio: bio.value })
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('profile.saveFailed')))
      return
    } finally {
      saving.value = false
    }
  }
  emit('done')
}
</script>

<template>
  <form class="space-y-4" data-testid="welcome-profile" @submit.prevent="save">
    <div class="flex items-center gap-4">
      <UserAvatar :user="auth.user" size="xl" />
      <button
        type="button"
        class="px-3 py-1.5 rounded-lg bg-mnema-accent/15 hover:bg-mnema-accent/25 text-mnema-accent text-sm font-medium border border-mnema-accent/30 flex items-center gap-1.5 transition disabled:opacity-50"
        :disabled="uploading"
        @click="fileInput?.click()"
      >
        <Loader2 v-if="uploading" class="w-4 h-4 animate-spin" />
        <Camera v-else class="w-4 h-4" />
        <span>{{ $t('profile.changeAvatar') }}</span>
      </button>
      <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" data-testid="welcome-avatar" @change="onAvatar" />
    </div>
    <div>
      <label for="welcome-displayname" class="text-xs text-mnema-tertiary block mb-0.5">{{ $t('profile.displayName') }}</label>
      <input
        id="welcome-displayname"
        v-model="displayName"
        type="text"
        maxlength="24"
        class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-hidden focus:border-mnema-accent"
      />
    </div>
    <div>
      <label for="welcome-bio" class="text-xs text-mnema-tertiary block mb-0.5">{{ $t('profile.bioLabel', { count: 250 }) }}</label>
      <textarea
        id="welcome-bio"
        v-model="bio"
        rows="3"
        maxlength="250"
        class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-hidden focus:border-mnema-accent resize-none"
        :placeholder="$t('profile.bioPlaceholder')"
      ></textarea>
    </div>
    <div class="flex justify-end gap-2">
      <button type="button" class="btn btn-ghost" data-testid="welcome-skip" @click="emit('skip')">
        {{ $t('welcome.skip') }}
      </button>
      <button type="submit" :disabled="saving" class="btn btn-primary" data-testid="welcome-profile-save">
        {{ $t('welcome.continue') }}
      </button>
    </div>
  </form>
</template>
