<script setup lang="ts">
import { ref, watch } from 'vue'
import type { CommunityEvent } from '../types/domain'
import { useEventRoles } from '../composables/useEventRoles'
import { useRosterDraft } from '../composables/useRosterDraft'
import { useSettingsDraft } from '../composables/useSettingsDraft'
import { caughtErrorMessage } from '../lib/api'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import EventRolesEditor from './EventRolesEditor.vue'
import ScopeChoice from './ScopeChoice.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'
import type { Scope } from '../lib/eventRecurrence'

// The role editor on its own, for the creator of an event who may change the
// roster without being allowed to edit the event. The roster is the draft: Save is
// on when it differs from the event's roles, and closing with changes asks first.
const props = defineProps<{ event: CommunityEvent }>()
const emit = defineEmits<{ close: [] }>()

const toast = useToastStore()
const roster = useRosterDraft(() => props.event.roles)
const scope = ref<Scope>('this')
const { save: saveRoles } = useEventRoles()

const form = useSettingsDraft<{ roster: string }>({
  source: () => ({ roster: roster.savedKey.value }),
  save: async () => {
    try {
      await saveRoles(props.event.id, roster.rows.value, roster.removals.value, roster.sync, props.event.series ? scope.value : undefined)
      roster.removals.value = []
    } catch (err) {
      throw new Error(caughtErrorMessage(err, t('events.roles.saveFailed')))
    }
  },
  saved: () => {
    toast.success(t('events.roles.saved'))
    emit('close')
  }
})
watch(roster.key, key => { form.draft.roster = key })

function reset() {
  form.reset()
  roster.reset()
}
</script>

<template>
  <BaseDialog :title="$t('events.roles.dialogTitle')" panel-class="max-w-lg" sheet :guard="form.guard" @close="emit('close')">
    <form class="flex min-h-0 flex-1 flex-col" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6 max-md:p-4">
        <EventRolesEditor v-model="roster.rows.value" v-model:removals="roster.removals.value" />
        <ScopeChoice v-if="event.series" v-model="scope" :hint="$t('events.scope.rolesHint')" />
      </div>
      <UnsavedChangesBar
        :dirty="form.dirty.value"
        :can-save="form.canSave.value"
        :saving="form.saving.value"
        :error="form.error.value"
        save-testid="event-roles-save"
        @save="form.save"
        @reset="reset"
      />
    </form>
  </BaseDialog>
</template>
