<script setup lang="ts">
import { ref } from 'vue'
import { Trash2 } from '@lucide/vue'
import type { RoleDraft } from '../lib/eventRoleDrafts'
import { inputsOf } from '../lib/eventRoleDrafts'
import { createRoleTemplate, deleteRoleTemplate, setRoleTemplateEnabled } from '../lib/eventRolesApi'
import type { RoleTemplate } from '../lib/eventRolesApi'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// For administrators only: which role templates members can pick. The presets
// that ship with the server stay hidden until they are turned on here. These are
// actions, not fields of the event: each button runs when pressed, asks first
// when it deletes, and confirms with a toast. They never wait for the form's Save.
const props = defineProps<{ templates: RoleTemplate[]; rows: RoleDraft[] }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToastStore()
const name = ref('')
const busy = ref(false)

async function run(work: () => Promise<unknown>, done: string): Promise<boolean> {
  if (busy.value) return false
  busy.value = true
  try {
    await work()
    toast.success(done)
    emit('changed')
    return true
  } catch (err) {
    toast.error(caughtErrorMessage(err, t('events.roles.templateFailed')))
    return false
  } finally {
    busy.value = false
  }
}

const toggle = (tpl: RoleTemplate) => run(() => setRoleTemplateEnabled(tpl.id, !tpl.enabled), t(tpl.enabled ? 'events.roles.templateOff' : 'events.roles.templateOn', { name: tpl.name }))

async function remove(tpl: RoleTemplate) {
  const ok = await confirm({ title: t('events.roles.deleteTemplateTitle'), body: t('events.roles.deleteTemplateBody'), excerpt: tpl.name, confirmLabel: t('common.delete'), danger: true })
  if (ok) await run(() => deleteRoleTemplate(tpl.id), t('events.roles.templateDeleted', { name: tpl.name }))
}

async function saveCurrent() {
  const roles = inputsOf(props.rows, false)
  const label = name.value.trim()
  if (!roles || roles.length === 0 || !label) return
  // The name stays when saving failed, so it can be tried again.
  if (await run(() => createRoleTemplate(label, roles), t('events.roles.templateSaved', { name: label }))) name.value = ''
}
</script>

<template>
  <details class="rounded-lg border border-mnema-hairline p-3" data-testid="event-role-templates">
    <summary class="cursor-pointer text-sm font-medium text-mnema-text max-md:min-h-11">{{ $t('events.roles.templates') }}</summary>
    <p class="mt-2 text-xs leading-relaxed text-mnema-tertiary">{{ $t('events.roles.templatesHint') }}</p>
    <ul class="mt-3 flex flex-col gap-3">
      <li v-for="tpl in templates" :key="tpl.id" class="flex items-start gap-2" data-testid="event-role-template-row">
        <div class="min-w-0 flex-1 space-y-0.5">
          <div class="flex items-center gap-2 text-sm font-semibold text-mnema-text">
            {{ tpl.name }}
            <span :class="['rounded-full px-2 py-0.5 text-[11px] font-normal', tpl.enabled ? 'bg-mnema-accent/15 text-mnema-accent' : 'bg-mnema-hover text-mnema-tertiary']" data-testid="event-template-state">{{ tpl.enabled ? $t('events.roles.stateOn') : $t('events.roles.stateOff') }}</span>
          </div>
          <p class="text-xs leading-relaxed text-mnema-tertiary">{{ tpl.roles.map(r => `${r.name}${r.capacity ? ' ' + r.capacity : ''}`).join(' · ') }}</p>
        </div>
        <button type="button" class="btn btn-secondary shrink-0 px-3 py-1.5 text-xs max-md:min-h-11" data-testid="event-template-toggle" :disabled="busy" :aria-label="$t(tpl.enabled ? 'events.roles.turnOffTemplate' : 'events.roles.turnOnTemplate', { name: tpl.name })" @click="toggle(tpl)">{{ tpl.enabled ? $t('events.roles.turnOff') : $t('events.roles.turnOn') }}</button>
        <button v-if="!tpl.preset" type="button" class="btn btn-icon h-8 w-8 shrink-0 hover:text-red-400 max-md:h-11 max-md:w-11" :disabled="busy" :aria-label="$t('events.roles.deleteTemplate', { name: tpl.name })" @click="remove(tpl)"><Trash2 class="h-4 w-4" /></button>
      </li>
    </ul>
    <div class="mt-3 flex flex-wrap items-center gap-2">
      <input v-model="name" type="text" v-max-chars="60" data-testid="event-template-name" :aria-label="$t('events.roles.templateName')" :placeholder="$t('events.roles.templateName')" class="h-9 min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-surface px-3 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden max-md:h-11" @keydown.enter.prevent.stop="saveCurrent" />
      <button type="button" data-testid="event-template-save" class="btn btn-secondary px-3 py-1.5 text-xs max-md:min-h-11" :disabled="busy || !name.trim() || rows.length === 0" @click="saveCurrent">{{ $t('events.roles.saveTemplate') }}</button>
    </div>
  </details>
</template>
