<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
import { computed, ref } from 'vue'
import { Edit3, Eraser, Loader2, Save } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { EXPIRY_CHOICES, expiryTime, isFuture, toLocalInput } from '../lib/statusExpiry'
import type { ExpiryChoice } from '../lib/statusExpiry'
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

// The feature list is loaded by the chat header; until then no expiry choice shows.
const serverFeatures = useServerFeaturesStore()
const canExpire = computed(() => props.isSelf && serverFeatures.has('status.expiry'))
const expiryChoice = ref<ExpiryChoice>('never')
const expiryCustom = ref('')
const expiryError = ref('')

const canEditStatus =computed(() => props.isSelf || (props.profileUser.role !== 'admin' && permissions.can('manage_members') && permissions.outranksMember(props.profileUser.id ?? '')))
const isEditingStatus = ref(false)
const editStatus = ref('')
const isSavingStatus = ref(false)

function startEditStatus() {
  editStatus.value = props.profileUser.status_text || ''
  expiryChoice.value = 'never'
  expiryCustom.value = toLocalInput(new Date(Date.now() + 24 * 3_600_000))
  expiryError.value = ''
  isEditingStatus.value = true
}

// The absolute clear time, from this device's clock and time zone; null: never.
function chosenExpiry(text: string): Date | null | undefined {
  if (!canExpire.value || !text || expiryChoice.value === 'never') return null
  const at = expiryTime(expiryChoice.value, new Date(), expiryCustom.value)
  if (isFuture(at, new Date())) return at
  expiryError.value = t('profile.expiryPast')
  return undefined
}

async function saveStatus(text = editStatus.value) {
  const id = props.profileUser.id
  if (!id) return
  expiryError.value = ''
  const expiresAt = chosenExpiry(text.trim())
  if (expiresAt === undefined) return
  isSavingStatus.value = true
  try {
    const updated = await chatStore.setStatusText(id, text.trim(), expiresAt)
    const selected = chatStore.selectedUserProfile
    if (selected) {
      const next = { ...selected, status_text: updated.status_text }
      if (updated.status_expires_at) next.status_expires_at = updated.status_expires_at
      else delete next.status_expires_at
      chatStore.selectedUserProfile = next
    }
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
    <form v-if="isEditingStatus" class="flex flex-wrap items-center gap-1.5" @submit.prevent="saveStatus()">
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
      <div v-if="canExpire && editStatus.trim()" class="flex w-full flex-wrap items-center gap-2 pt-1" data-testid="status-expiry">
        <label for="status-expiry-choice" class="text-xs text-mnema-tertiary">{{ $t('profile.clearAfter') }}</label>
        <select
          id="status-expiry-choice"
          v-model="expiryChoice"
          data-testid="status-expiry-choice"
          class="rounded-lg border border-mnema-border bg-mnema-canvas px-2 py-1 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        >
          <option v-for="c in EXPIRY_CHOICES" :key="c" :value="c">{{ $t(`profile.expiry.${c}`) }}</option>
        </select>
        <input
          v-if="expiryChoice === 'custom'"
          v-model="expiryCustom"
          type="datetime-local"
          data-testid="status-expiry-custom"
          :aria-label="$t('profile.clearAfter')"
          class="rounded-lg border border-mnema-border bg-mnema-canvas px-2 py-1 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        />
        <p v-if="expiryError" role="alert" class="w-full text-xs text-mnema-danger">{{ expiryError }}</p>
      </div>
    </form>
    <template v-else-if="profileUser.status_text">
      <p class="wrap-break-word text-sm text-mnema-text">{{ profileUser.status_text }}</p>
      <p v-if="profileUser.status_expires_at" data-testid="profile-status-until" class="mt-0.5 text-xs text-mnema-tertiary">
        {{ $t('profile.clearsAt', { time: new Date(profileUser.status_expires_at).toLocaleString() }) }}
      </p>
    </template>
    <p v-else class="text-sm italic text-mnema-tertiary">{{ $t('profile.noStatus') }}</p>
  </div>
</template>
