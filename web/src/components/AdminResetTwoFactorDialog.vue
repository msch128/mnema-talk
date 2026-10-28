<script setup lang="ts">
import type { PropType } from 'vue'
import type { AdminUser } from '../types/domain'
import { ref } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { ownerProof, useOwnerNeedsCode } from '../lib/ownerProof'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import IdentityFields from './IdentityFields.vue'

// The Owner turns off a member's two-factor sign-in, after confirming their own identity.
const props = defineProps({
  user: { type: Object as PropType<AdminUser>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const toasts = useToastStore()
const ownerPassword = ref('')
const ownerCode = ref('')
const error = ref('')
const busy = ref(false)
const needsCode = useOwnerNeedsCode()

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await api(`/api/admin/users/${props.user.id}/two-factor/reset`, {
      method: 'POST',
      json: ownerProof(ownerPassword.value, ownerCode.value)
    })
    toasts.success(t('admin.twoFactorReset'))
    emit('close')
  } catch (e) {
    error.value = caughtErrorMessage(e, t('admin.unknownError'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog
    :title="$t('admin.resetTwoFactorTitle', { name: user.display_name || user.username })"
    panel-class="max-w-md"
    @close="emit('close')"
  >
    <form class="p-6 space-y-4" @submit.prevent="submit">
      <p class="text-sm text-mnema-muted">{{ $t('admin.resetTwoFactorBody') }}</p>
      <p class="text-sm text-mnema-muted">{{ $t('admin.confirmOwnerPrompt') }}</p>
      <IdentityFields v-model:password="ownerPassword" v-model:code="ownerCode" :needs-code="needsCode" />
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger" data-testid="owner-confirm-error">{{ error }}</div>
      <div class="flex justify-end gap-2 pt-2">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy" class="btn btn-danger-solid" data-testid="reset-two-factor-submit">{{ $t('admin.resetTwoFactorConfirm') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
