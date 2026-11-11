<script setup lang="ts">
import { charCount } from '../../shared/lib/textLength'
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../../types/domain'
import { computed, ref } from 'vue'
import { Edit3, Eraser } from '@lucide/vue'
import { caughtErrorMessage } from '../../shared/lib/api'
import { useChatStore } from '../../stores/chat'
import { useToastStore } from '../../stores/toast'
import { usePermissionsStore } from '../../stores/permissions'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'
import { locale, t } from '../../i18n'
import { dateTime } from '../../shared/lib/time'

// The status line of a profile. One's own is written in the status dialog; an
// admin may change or clear anyone else's here.
const props = defineProps({
  profileUser: { type: Object as PropType<Partial<ProfileUser>>, required: true },
  isSelf: { type: Boolean, default: false }
})
// One's own status is written in the status dialog: the profile (which may hold other unsaved edits) decides whether it makes way.
const emit = defineEmits<{ editOwn: [] }>()

const MAX_STATUS = 32
const chatStore = useChatStore()
const toasts = useToastStore()
const permissions = usePermissionsStore()

const canEditStatus = computed(() => props.isSelf || (props.profileUser.role !== 'admin' && permissions.can('manage_members') && permissions.outranksMember(props.profileUser.id ?? '')))
const isEditingStatus = ref(false)
const isClearing = ref(false)

// The inline edit of another member's status is a small draft (doc/settings-ux.md):
// Save is on only for a change, and leaving it (Escape, closing the profile) asks first.
const form = useSettingsDraft<{ status: string }>({
  source: () => ({ status: props.profileUser.status_text || '' }),
  normalisers: { status: normalise.text },
  blocked: () => !props.profileUser.id,
  save: async ({ draft, commit }) => {
    try {
      await store(draft.status.trim())
    } catch (err) {
      throw new Error(caughtErrorMessage(err, t('profile.saveFailed')), { cause: err })
    }
    commit(['status'])
  },
  saved: () => { isEditingStatus.value = false }
})
const { draft } = form

async function store(text: string) {
  const id = props.profileUser.id
  if (!id) return
  const updated = await chatStore.setStatusText(id, text)
  chatStore.patchOpenProfile(id, { status_text: updated.status_text, ...(updated.status_expires_at ? { status_expires_at: updated.status_expires_at } : {}) }, updated.status_expires_at ? [] : ['status_expires_at'])
  toasts.success(text ? t('profile.statusSaved') : t('profile.statusCleared'))
}

function startEditStatus() {
  if (props.isSelf) {
    emit('editOwn')
    return
  }
  form.reset()
  isEditingStatus.value = true
}

async function cancelEditStatus() {
  if (!await form.guard()) return
  form.reset()
  isEditingStatus.value = false
}

async function clearStatus() {
  isClearing.value = true
  try {
    await store('')
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('profile.saveFailed')))
  } finally {
    isClearing.value = false
  }
}
</script>

<template>
  <div v-if="profileUser.status_text || canEditStatus || isEditingStatus" data-testid="profile-status">
    <div class="mb-2 flex min-h-6 items-center justify-between">
      <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('profile.status') }}</span>
      <div v-if="canEditStatus && !isEditingStatus" class="flex items-center gap-2">
        <button
          v-if="!isSelf && profileUser.status_text"
          type="button"
          data-testid="profile-status-clear"
          class="flex items-center gap-1 text-xs text-mnema-danger hover:underline"
          :disabled="isClearing"
          @click="clearStatus"
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
          <span>{{ isSelf ? (profileUser.status_text ? $t('profile.edit') : $t('presence.setStatus')) : $t('profile.moderateStatus') }}</span>
        </button>
      </div>
    </div>
    <form v-if="isEditingStatus" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="flex items-center gap-2">
        <input
          v-model="draft.status"
          data-testid="profile-status-input"
          type="text"
          v-max-chars="MAX_STATUS"
          :aria-label="$t('profile.status')"
          :placeholder="$t('profile.statusPlaceholder')"
          class="min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
          data-local-escape
          @keydown.esc.stop.prevent="cancelEditStatus"
        />
        <span class="w-10 text-right text-xs tabular-nums text-mnema-tertiary">{{ charCount(draft.status) }}/{{ MAX_STATUS }}</span>
      </div>
      <UnsavedChangesBar inline :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset">
        <template #extra><button type="button" class="btn btn-ghost btn-sm" data-testid="profile-status-cancel" @click="cancelEditStatus">{{ $t('common.cancel') }}</button></template>
      </UnsavedChangesBar>
    </form>
    <template v-else-if="profileUser.status_text">
      <p class="wrap-break-word text-sm text-mnema-text">{{ profileUser.status_text }}</p>
      <p v-if="profileUser.status_expires_at" data-testid="profile-status-until" class="mt-0.5 text-xs text-mnema-tertiary">
        {{ $t('profile.clearsAt', { time: dateTime(profileUser.status_expires_at, locale) }) }}
      </p>
    </template>
    <p v-else class="text-sm italic text-mnema-tertiary">{{ $t('profile.noStatus') }}</p>
  </div>
</template>
