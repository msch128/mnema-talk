<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { computed, ref } from 'vue'
import { Edit3, Eraser, Loader2, Save } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { t } from '../i18n'

// The status line of a profile: the user edits their own; an admin may edit or clear anyone's.
const props = defineProps({
  profileUser: { type: Object as PropType<Partial<ProfileUser>>, required: true },
  isSelf: { type: Boolean, default: false }
})

const MAX_STATUS = 32
const chatStore = useChatStore()
const toasts = useToastStore()
const permissions = usePermissionsStore()

const canEditStatus = computed(() => props.isSelf || (props.profileUser.role !== 'admin' && permissions.can('manage_members') && permissions.outranksMember(props.profileUser.id ?? '')))
const isEditingStatus = ref(false)
const editStatus = ref('')
const isSavingStatus = ref(false)

function startEditStatus() {
  editStatus.value = props.profileUser.status_text || ''
  isEditingStatus.value = true
}

async function saveStatus(text = editStatus.value) {
  const id = props.profileUser.id
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
</script>

<template>
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
        class="min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        @keydown.esc.stop.prevent="isEditingStatus = false"
      />
      <span class="w-10 text-right text-xs tabular-nums text-mnema-tertiary">{{ editStatus.length }}/{{ MAX_STATUS }}</span>
      <button
        type="submit"
        :disabled="isSavingStatus"
        class="btn btn-primary btn-sm flex items-center gap-1"
      >
        <Loader2 v-if="isSavingStatus" class="h-3.5 w-3.5 animate-spin" />
        <Save v-else class="h-3.5 w-3.5" />
        <span>{{ $t('common.save') }}</span>
      </button>
    </form>
    <p v-else-if="profileUser.status_text" class="wrap-break-word text-sm text-mnema-text">{{ profileUser.status_text }}</p>
    <p v-else class="text-sm italic text-mnema-tertiary">{{ $t('profile.noStatus') }}</p>
  </div>
</template>
