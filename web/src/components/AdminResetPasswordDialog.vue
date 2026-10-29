<script setup lang="ts">
import type { PropType } from 'vue'
import type { AdminUser } from '../types/domain'
import { ref, computed } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { ownerProof, useOwnerNeedsCode } from '../lib/ownerProof'
import BaseDialog from './BaseDialog.vue'
import PasswordInput from './PasswordInput.vue'
import IdentityFields from './IdentityFields.vue'

// The Owner sets a new password for an account.
const props = defineProps({
  user: { type: Object as PropType<AdminUser>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const toasts = useToastStore()
const newPasswordInput = ref('')
const isResettingPassword = ref(false)
const ownerPassword = ref('')
const ownerCode = ref('')
const error = ref('')
const needsCode = useOwnerNeedsCode()

// Counted in characters like the server does, not UTF-16 units.
const passwordTooShort = computed(() => [...newPasswordInput.value].length < MIN_PASSWORD_LENGTH)

async function submitPasswordReset() {
  if (passwordTooShort.value) return
  error.value = ''
  isResettingPassword.value = true
  try {
    await api(`/api/admin/users/${props.user.id}/password`, {
      method: 'POST',
      json: { password: newPasswordInput.value, ...ownerProof(ownerPassword.value, ownerCode.value) }
    })
    toasts.success(t('admin.passwordResetSuccess'))
    emit('close')
  } catch (e) {
    error.value = caughtErrorMessage(e, t('admin.unknownError'))
  } finally {
    isResettingPassword.value = false
  }
}
</script>

<template>
  <BaseDialog
    :title="$t('admin.resetPasswordTitle', { name: user.display_name || user.username })"
    panel-class="max-w-md"
    @close="emit('close')"
  >
    <form @submit.prevent="submitPasswordReset" class="p-6 space-y-4">
      <p class="text-sm text-mnema-muted">{{ $t('admin.resetPasswordPrompt', { count: MIN_PASSWORD_LENGTH }) }}</p>
      <div>
        <label for="admin-new-password" class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
          {{ $t('admin.newPasswordLabel') }}
        </label>
        <PasswordInput
          id="admin-new-password"
          v-model="newPasswordInput"
          data-testid="new-password-input"
          autocomplete="new-password"
          :placeholder="$t('admin.newPasswordPlaceholder', { count: MIN_PASSWORD_LENGTH })"
        />
        <p v-if="newPasswordInput && passwordTooShort" class="text-xs text-mnema-danger mt-1">
          {{ $t('admin.passwordTooShort', { count: MIN_PASSWORD_LENGTH }) }}
        </p>
      </div>
      <p class="text-sm text-mnema-muted">{{ $t('admin.confirmOwnerPrompt') }}</p>
      <IdentityFields v-model:password="ownerPassword" v-model:code="ownerCode" :needs-code="needsCode" />
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger" data-testid="owner-confirm-error">{{ error }}</div>
      <div class="flex justify-end gap-2 pt-2">
        <button
          type="button"
          class="btn btn-secondary"
          @click="emit('close')"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          data-testid="save-password-button"
          type="submit"
          :disabled="passwordTooShort || isResettingPassword"
          class="btn btn-primary"
        >
          {{ $t('admin.savePassword') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
