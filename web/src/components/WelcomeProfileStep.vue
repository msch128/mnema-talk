<script setup lang="ts">
// Welcome flow, profile step: avatar, display name and bio through the same
// store calls as the profile dialog. Every field is optional.
import { ref } from 'vue'
import { Camera, Loader2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { IMAGE_UPLOAD_MAX_MB, pickImage } from '../lib/pickImage'
import FormField from './FormField.vue'
import UserAvatar from './UserAvatar.vue'

const emit = defineEmits<{ done: []; skip: [] }>()
const auth = useAuthStore()
const toasts = useToastStore()

const displayName = ref(auth.user?.display_name ?? '')
const bio = ref(auth.user?.bio ?? '')
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const saving = ref(false)

async function onAvatar(e: Event) {
  const { file, tooBig } = pickImage(e.target as HTMLInputElement)
  if (tooBig) toasts.error(t('profile.avatarTooBig', { size: IMAGE_UPLOAD_MAX_MB }))
  if (!file) return
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
  <form class="space-y-5" data-testid="welcome-profile" @submit.prevent="save">
    <div class="flex items-center gap-4">
      <button
        type="button"
        class="group relative shrink-0 rounded-full focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent"
        :aria-label="$t('profile.changeAvatar')"
        :disabled="uploading"
        data-testid="welcome-avatar-button"
        @click="fileInput?.click()"
      >
        <UserAvatar :user="auth.user" size="xl" />
        <span class="absolute inset-0 flex items-center justify-center rounded-full bg-black/55 text-white opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100 max-md:opacity-100">
          <Loader2 v-if="uploading" class="h-5 w-5 animate-spin" aria-hidden="true" />
          <Camera v-else class="h-5 w-5" aria-hidden="true" />
        </span>
      </button>
      <p class="text-sm text-mnema-muted">{{ $t('welcome.profile.avatarHint') }}</p>
      <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" data-testid="welcome-avatar" @change="onAvatar" />
    </div>
    <FormField v-slot="{ id }" input-id="welcome-displayname" :label="$t('profile.displayName')">
      <input
        :id="id"
        v-model="displayName"
        type="text"
        v-max-chars="24"
        class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
      />
    </FormField>
    <FormField v-slot="{ id }" input-id="welcome-bio" :label="$t('profile.bioLabel', { count: 250 })">
      <textarea
        :id="id"
        v-model="bio"
        rows="3"
        v-max-chars="250"
        class="w-full resize-none rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
        :placeholder="$t('profile.bioPlaceholder')"
      ></textarea>
    </FormField>
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
