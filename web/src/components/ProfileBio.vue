<script setup lang="ts">
import { charCount } from '../lib/textLength'
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { computed, ref } from 'vue'
import { Edit3, Loader2, Save } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import FormField from './FormField.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// "About me": the bio and display name, editable on one's own profile.
const props = defineProps({
  profileUser: { type: Object as PropType<Partial<ProfileUser>>, required: true },
  isSelf: { type: Boolean, default: false }
})

const authStore = useAuthStore()
const chatStore = useChatStore()
const toasts = useToastStore()

const isEditingBio = ref(false)
const editDisplayName = ref('')
const editBio = ref('')
const isSavingProfile = ref(false)
const profileSaveError = ref('')

// The profile opens with a stub (no bio) that fills in once /api/users/:id
// answers, so the edit fields are copied when editing starts. Your own
// account record is complete, so it wins; without a loaded bio there is
// nothing safe to send (the PUT replaces both fields).
const editSource = computed(() => {
  const own = authStore.user
  if (props.isSelf && own && 'bio' in own) return own
  const p = props.profileUser
  return 'bio' in p ? p : null
})
const canEditProfile = computed(() => props.isSelf && !!editSource.value)

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
  const openId = chatStore.selectedUserProfile?.id
  try {
    const updated = await authStore.updateProfile({
      displayName: editDisplayName.value,
      bio: editBio.value
    })
    chatStore.patchOpenProfile(openId, updated)
    isEditingBio.value = false
    toasts.success(t('profile.profileSaved'))
  } catch (err) {
    profileSaveError.value = caughtErrorMessage(err, t('profile.saveFailed'))
  } finally {
    isSavingProfile.value = false
  }
}
</script>

<template>
  <div>
    <div class="mb-2 flex min-h-6 items-center justify-between">
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
    <form v-else class="space-y-4" @submit.prevent="saveProfile">
      <FormField v-slot="{ id, describedby }" input-id="profile-displayname" :label="$t('profile.displayName')" :hint="$t('profile.displayNameHint', { username: profileUser.username, count: 24 })">
        <input
          :id="id"
          v-model="editDisplayName"
          :aria-describedby="describedby"
          type="text"
          v-max-chars="24"
          class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text max-md:min-h-11"
        />
      </FormField>
      <FormField v-slot="{ id }" input-id="profile-bio" :label="$t('profile.bioLabel', { count: 250 })">
        <textarea
          :id="id"
          v-model="editBio"
          rows="4"
          v-max-chars="250"
          class="w-full resize-none rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
          :placeholder="$t('profile.bioPlaceholder')"
        ></textarea>
        <p class="text-right text-xs tabular-nums text-mnema-tertiary">{{ charCount(editBio) }} / 250</p>
      </FormField>
      <div class="flex items-center justify-end gap-2">
        <button type="button" class="btn btn-ghost btn-sm max-md:min-h-11" @click="cancelEditBio">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="isSavingProfile" class="btn btn-primary btn-sm flex items-center gap-1 max-md:min-h-11">
          <Loader2 v-if="isSavingProfile" class="w-3.5 h-3.5 animate-spin" />
          <Save v-else class="w-3.5 h-3.5" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </div>
    </form>

    <div v-if="profileSaveError" role="alert" class="mt-2 p-1.5 rounded-sm bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-1.5">
      <span>{{ profileSaveError }}</span>
    </div>
  </div>
</template>
