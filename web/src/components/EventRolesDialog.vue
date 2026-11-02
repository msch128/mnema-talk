<script setup lang="ts">
import { ref } from 'vue'
import { Loader2 } from '@lucide/vue'
import type { CommunityEvent } from '../types/domain'
import { draftsOf } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import { useEventRoles } from '../composables/useEventRoles'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import EventRolesEditor from './EventRolesEditor.vue'

// The role editor on its own, for the creator of an event who may change the
// roster without being allowed to edit the event.
const props = defineProps<{ event: CommunityEvent }>()
const emit = defineEmits<{ close: [] }>()

const rows = ref<RoleDraft[]>(draftsOf(props.event.roles ?? []))
const removals = ref<RoleRemoval[]>([])
const saving = ref(false)
const error = ref('')
const { save } = useEventRoles()

async function submit() {
  if (saving.value) return
  saving.value = true
  error.value = ''
  try {
    await save(props.event.id, rows.value, removals.value)
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('events.roles.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseDialog :title="$t('events.roles.dialogTitle')" panel-class="max-w-lg" sheet @close="emit('close')">
    <form class="flex flex-col gap-4 overflow-y-auto p-6 max-md:p-4" @submit.prevent="submit">
      <div v-if="error" role="alert" class="rounded-lg bg-red-500/10 p-3 text-sm text-red-400">{{ error }}</div>
      <EventRolesEditor v-model="rows" v-model:removals="removals" />
      <div class="flex justify-end gap-3 border-t border-mnema-hairline pt-4">
        <button type="button" class="rounded-lg border border-mnema-border px-4 py-2 text-sm font-medium text-mnema-text hover:bg-mnema-hover max-md:min-h-11" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" data-testid="event-roles-save" :disabled="saving" class="flex items-center gap-2 rounded-lg bg-mnema-accent px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50 max-md:min-h-11">
          <Loader2 v-if="saving" class="h-4 w-4 animate-spin" />{{ $t('common.save') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
