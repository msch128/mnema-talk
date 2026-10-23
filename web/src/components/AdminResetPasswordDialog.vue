<script setup lang="ts">
import type { PropType } from 'vue'
import type { AdminUser } from '../types/domain'
import { ref, computed } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

// The Owner sets a new password for an account.
const props = defineProps({
  user: { type: Object as PropType<AdminUser>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const toasts = useToastStore()
const newPasswordInput = ref('')
const isResettingPassword = ref(false)

// Counted in characters like the server does, not UTF-16 units.
const passwordTooShort = computed(() => [...newPasswordInput.value].length < MIN_PASSWORD_LENGTH)

async function submitPasswordReset() {
  if (passwordTooShort.value) return
  isResettingPassword.value = true
  try {
    await api(`/api/admin/users/${props.user.id}/password`, {
      method: 'POST',
      json: { password: newPasswordInput.value }
    })
    toasts.success(t('admin.passwordResetSuccess'))
    emit('close')
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
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
        <label class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
          {{ $t('admin.newPasswordLabel') }}
        </label>
        <input
          v-model="newPasswordInput"
          data-testid="new-password-input"
          type="password"
          :placeholder="$t('admin.newPasswordPlaceholder', { count: MIN_PASSWORD_LENGTH })"
          :aria-label="$t('admin.newPasswordLabel')"
          class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-hidden focus:border-mnema-accent transition"
        />
        <p v-if="newPasswordInput && passwordTooShort" class="text-xs text-mnema-danger mt-1">
          {{ $t('admin.passwordTooShort', { count: MIN_PASSWORD_LENGTH }) }}
        </p>
      </div>
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
