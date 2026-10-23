<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { computed, ref } from 'vue'
import { Edit3, Loader2, Save } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
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
</script>

<template>
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
          class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-hidden focus:border-mnema-accent"
        />
      </div>
      <div>
        <label for="profile-bio" class="text-xs text-mnema-tertiary block mb-0.5">{{ $t('profile.bioLabel', { count: 250 }) }}</label>
        <textarea
          id="profile-bio"
          v-model="editBio"
          rows="3"
          maxlength="250"
          class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-hidden focus:border-mnema-accent resize-none"
          :placeholder="$t('profile.bioPlaceholder')"
        ></textarea>
        <div class="flex justify-between items-center text-xs text-mnema-tertiary mt-0.5">
          <span>{{ editBio.length }} / 250</span>
          <div class="flex items-center gap-1.5">
            <button
              type="button"
              @click="cancelEditBio"
              class="px-2 py-0.5 rounded-sm text-mnema-muted hover:text-mnema-text"
            >
              {{ $t('common.cancel') }}
            </button>
            <button
              @click="saveProfile"
              :disabled="isSavingProfile"
              class="px-2.5 py-1 rounded-sm bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50"
            >
              <Loader2 v-if="isSavingProfile" class="w-3.5 h-3.5 animate-spin" />
              <Save v-else class="w-3.5 h-3.5" />
              <span>{{ $t('common.save') }}</span>
            </button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="profileSaveError" role="alert" class="mt-2 p-1.5 rounded-sm bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-1.5">
      <span>{{ profileSaveError }}</span>
    </div>
  </div>
</template>
