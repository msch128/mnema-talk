<script setup lang="ts">
import { ref } from 'vue'
import { Trash2 } from '@lucide/vue'
import type { RoleDraft } from '../lib/eventRoleDrafts'
import { inputsOf } from '../lib/eventRoleDrafts'
import { createRoleTemplate, deleteRoleTemplate, setRoleTemplateEnabled } from '../lib/eventRolesApi'
import type { RoleTemplate } from '../lib/eventRolesApi'
import { caughtErrorMessage } from '../lib/api'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import SwitchRow from './SwitchRow.vue'

// For administrators only: which role templates members can pick. The presets
// that ship with the server stay hidden until they are switched on here.
const props = defineProps<{ templates: RoleTemplate[]; rows: RoleDraft[] }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToastStore()
const name = ref('')
const busy = ref(false)

async function run(work: () => Promise<unknown>, failure: string) {
  if (busy.value) return
  busy.value = true
  try {
    await work()
    emit('changed')
  } catch (err) {
    toast.error(caughtErrorMessage(err, failure))
  } finally {
    busy.value = false
  }
}

const toggle = (tpl: RoleTemplate, enabled: boolean) => run(() => setRoleTemplateEnabled(tpl.id, enabled), t('events.roles.templateFailed'))
const remove = (tpl: RoleTemplate) => run(() => deleteRoleTemplate(tpl.id), t('events.roles.templateFailed'))

async function saveCurrent() {
  const roles = inputsOf(props.rows, false)
  if (!roles || roles.length === 0 || !name.value.trim()) return
  await run(() => createRoleTemplate(name.value.trim(), roles), t('events.roles.templateFailed'))
  name.value = ''
}
</script>

<template>
  <details class="rounded-lg border border-mnema-hairline p-3" data-testid="event-role-templates">
    <summary class="cursor-pointer text-sm font-medium text-mnema-text max-md:min-h-11">{{ $t('events.roles.templates') }}</summary>
    <p class="mt-2 text-xs leading-relaxed text-mnema-tertiary">{{ $t('events.roles.templatesHint') }}</p>
    <ul class="mt-3 flex flex-col gap-3">
      <li v-for="tpl in templates" :key="tpl.id" class="flex items-start gap-2">
        <SwitchRow class="min-w-0 flex-1" :model-value="tpl.enabled" :title="tpl.name" :hint="tpl.roles.map(r => `${r.name}${r.capacity ? ' ' + r.capacity : ''}`).join(' · ')" :disabled="busy" @toggle="toggle(tpl, $event)" />
        <button v-if="!tpl.preset" type="button" class="btn btn-icon h-8 w-8 shrink-0 hover:text-red-400 max-md:h-11 max-md:w-11" :aria-label="$t('events.roles.deleteTemplate', { name: tpl.name })" @click="remove(tpl)"><Trash2 class="h-4 w-4" /></button>
      </li>
    </ul>
    <div class="mt-3 flex flex-wrap items-center gap-2">
      <input v-model="name" type="text" v-max-chars="60" data-testid="event-template-name" :aria-label="$t('events.roles.templateName')" :placeholder="$t('events.roles.templateName')" class="h-9 min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-surface px-3 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden max-md:h-11" />
      <button type="button" data-testid="event-template-save" class="btn btn-secondary px-3 py-1.5 text-xs max-md:min-h-11" :disabled="busy || !name.trim() || rows.length === 0" @click="saveCurrent">{{ $t('events.roles.saveTemplate') }}</button>
    </div>
  </details>
</template>
