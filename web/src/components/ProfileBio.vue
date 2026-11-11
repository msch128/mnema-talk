<script setup lang="ts">
import { charCount } from '../shared/lib/textLength'
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { computed, ref } from 'vue'
import { Edit3 } from '@lucide/vue'
import { caughtErrorMessage } from '../shared/lib/api'
import { normalise, useSettingsDraft } from '../shared/composables/useSettingsDraft'
import FormField from '../shared/components/FormField.vue'
import UnsavedChangesBar from '../shared/components/UnsavedChangesBar.vue'
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

// The profile opens with a stub (no bio) that fills in once /api/users/:id
// answers, so editing starts only from a loaded record. Your own account
// record is complete, so it wins; without a loaded bio there is nothing safe
// to send (the PUT replaces both fields).
const editSource = computed(() => {
  const own = authStore.user
  if (props.isSelf && own && 'bio' in own) return own
  const p = props.profileUser
  return 'bio' in p ? p : null
})
const canEditProfile = computed(() => props.isSelf && !!editSource.value)

// Name and bio are one draft with one Save (doc/settings-ux.md): the PUT replaces both.
const form = useSettingsDraft<{ displayName: string; bio: string }>({
  source: () => ({ displayName: editSource.value?.display_name || '', bio: editSource.value?.bio || '' }),
  normalisers: { displayName: normalise.text, bio: normalise.text },
  blocked: () => !canEditProfile.value,
  save: async ({ draft, commit }) => {
    const openId = chatStore.selectedUserProfile?.id
    try {
      const updated = await authStore.updateProfile({ displayName: draft.displayName.trim(), bio: draft.bio.trim() })
      chatStore.patchOpenProfile(openId, updated)
    } catch (err) {
      throw new Error(caughtErrorMessage(err, t('profile.saveFailed')), { cause: err })
    }
    commit(['displayName', 'bio'])
  },
  saved: () => {
    isEditingBio.value = false
    toasts.success(t('profile.profileSaved'))
  }
})
const { draft } = form

function startEditBio() {
  if (!editSource.value) return
  form.reset()
  isEditingBio.value = true
}

async function cancelEditBio() {
  if (!await form.guard()) return
  form.reset()
  isEditingBio.value = false
}

// Closing the profile with an unsaved edit asks first.
defineExpose({ guard: form.guard })
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
    <form v-else class="space-y-4" @submit.prevent="form.save" @keydown="form.onKeydown">
      <FormField v-slot="{ id, describedby }" input-id="profile-displayname" :label="$t('profile.displayName')" :hint="$t('profile.displayNameHint', { username: profileUser.username, count: 24 })">
        <input
          :id="id"
          v-model="draft.displayName"
          :aria-describedby="describedby"
          type="text"
          v-max-chars="24"
          class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text max-md:min-h-11"
        />
      </FormField>
      <FormField v-slot="{ id }" input-id="profile-bio" :label="$t('profile.bioLabel', { count: 250 })">
        <textarea
          :id="id"
          v-model="draft.bio"
          rows="4"
          v-max-chars="250"
          class="w-full resize-none rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
          :placeholder="$t('profile.bioPlaceholder')"
        ></textarea>
        <p class="text-right text-xs tabular-nums text-mnema-tertiary">{{ charCount(draft.bio) }} / 250</p>
      </FormField>
      <UnsavedChangesBar inline :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset">
        <template #extra><button type="button" class="btn btn-ghost btn-sm max-md:min-h-11" data-testid="profile-bio-cancel" @click="cancelEditBio">{{ $t('common.cancel') }}</button></template>
      </UnsavedChangesBar>
    </form>

  </div>
</template>
