<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowDown, ArrowUp, Plus, Trash2 } from '@lucide/vue'
import { blankDraft, MAX_EVENT_ROLES, MAX_ROLE_CAPACITY, withTemplate } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import { fetchRoleTemplates } from '../lib/eventRolesApi'
import type { RoleTemplate } from '../lib/eventRolesApi'
import { usePermissionsStore } from '../stores/permissions'
import EventRoleTemplates from './EventRoleTemplates.vue'

// The "Roles & slots" editor: rows with an emoji, a name and a capacity (or none),
// in the order they are shown. A role that has members cannot just go: the
// editor asks where they move to and records it as a removal.
const rows = defineModel<RoleDraft[]>({ required: true })
const removals = defineModel<RoleRemoval[]>('removals', { required: true })

const permissions = usePermissionsStore()
const templates = ref<RoleTemplate[]>([])
const asking = ref<string | null>(null)
const moveTo = ref('')

const isAdmin = computed(() => permissions.can('administrator'))
const enabledTemplates = computed(() => templates.value.filter(t => t.enabled))
const canAdd = computed(() => rows.value.length < MAX_EVENT_ROLES)
// Rows that exist already and are not the one being removed can take its members.
const targets = computed(() => rows.value.filter(r => r.id && r.key !== asking.value))

async function loadTemplates() {
  try {
    templates.value = await fetchRoleTemplates()
  } catch (err) {
    console.warn('Failed to load the role templates', err)
  }
}
onMounted(loadTemplates)

function add() {
  if (canAdd.value) rows.value = [...rows.value, blankDraft()]
}

function move(index: number, by: -1 | 1) {
  const next = [...rows.value]
  const other = index + by
  if (other < 0 || other >= next.length) return
  ;[next[index], next[other]] = [next[other]!, next[index]!]
  rows.value = next
}

function setUnlimited(row: RoleDraft, unlimited: boolean) {
  row.capacity = unlimited ? null : Math.max(row.members, 1)
}

function remove(row: RoleDraft) {
  if (row.id && row.members > 0) {
    asking.value = row.key
    moveTo.value = ''
    return
  }
  rows.value = rows.value.filter(r => r.key !== row.key)
}

function confirmMove(row: RoleDraft) {
  if (!row.id || !moveTo.value) return
  removals.value = [...removals.value, { id: row.id, name: row.name, moveTo: moveTo.value }]
  rows.value = rows.value.filter(r => r.key !== row.key)
  asking.value = null
}

function pickTemplate(e: Event) {
  const select = e.target as HTMLSelectElement
  const template = enabledTemplates.value.find(t => t.id === select.value)
  select.value = ''
  if (template) rows.value = withTemplate(rows.value, template.roles)
}
</script>

<template>
  <div class="flex flex-col gap-3" data-testid="event-roles-editor">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-sm font-medium text-mnema-text">{{ $t('events.roles.editorTitle') }}</span>
      <span class="text-xs tabular-nums text-mnema-tertiary">{{ rows.length }}/{{ MAX_EVENT_ROLES }}</span>
    </div>
    <p class="text-xs leading-relaxed text-mnema-tertiary">{{ $t('events.roles.editorHint') }}</p>

    <ul class="flex flex-col gap-2">
      <li v-for="(row, i) in rows" :key="row.key" class="rounded-lg border border-mnema-hairline bg-mnema-canvas/60 p-2.5" data-testid="event-role-row">
        <div class="flex flex-wrap items-center gap-2">
          <input v-model="row.emoji" type="text" v-max-chars="32" :aria-label="$t('events.roles.emoji')" :placeholder="$t('events.roles.emoji')" class="h-9 w-14 rounded-lg border border-mnema-border bg-mnema-surface px-2 text-center text-sm text-mnema-text focus:outline-hidden max-md:h-11" />
          <input v-model="row.name" type="text" v-max-chars="40" data-testid="event-role-name" :aria-label="$t('events.roles.name')" :placeholder="$t('events.roles.name')" class="h-9 min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-surface px-3 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden max-md:h-11" />
          <div class="flex items-center">
            <button type="button" class="btn btn-icon h-8 w-8 max-md:h-11 max-md:w-11" :disabled="i === 0" :aria-label="$t('events.roles.moveUp')" @click="move(i, -1)"><ArrowUp class="h-4 w-4" /></button>
            <button type="button" class="btn btn-icon h-8 w-8 max-md:h-11 max-md:w-11" :disabled="i === rows.length - 1" :aria-label="$t('events.roles.moveDown')" @click="move(i, 1)"><ArrowDown class="h-4 w-4" /></button>
            <button type="button" class="btn btn-icon h-8 w-8 hover:text-red-400 max-md:h-11 max-md:w-11" :aria-label="$t('events.roles.removeRole', { name: row.name || '…' })" @click="remove(row)"><Trash2 class="h-4 w-4" /></button>
          </div>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-3 text-sm text-mnema-secondary">
          <label class="flex items-center gap-2 max-md:min-h-11">
            <input type="checkbox" class="accent-mnema-accent" :checked="row.capacity === null" @change="setUnlimited(row, ($event.target as HTMLInputElement).checked)" />
            {{ $t('events.roles.unlimited') }}
          </label>
          <label v-if="row.capacity !== null" class="flex items-center gap-2">
            {{ $t('events.roles.capacity') }}
            <input v-model.number="row.capacity" type="number" min="1" :max="MAX_ROLE_CAPACITY" data-testid="event-role-capacity" class="h-9 w-20 rounded-lg border border-mnema-border bg-mnema-surface px-2 text-sm text-mnema-text focus:outline-hidden max-md:h-11" />
          </label>
          <span v-if="row.members > 0" class="text-xs text-mnema-tertiary">{{ $t('events.roles.members', { count: row.members }) }}</span>
        </div>

        <div v-if="asking === row.key" class="mt-2 flex flex-wrap items-center gap-2 rounded-md bg-amber-400/10 p-2 text-sm text-amber-300" data-testid="event-role-move">
          <span>{{ $t('events.roles.moveMembers', { count: row.members }) }}</span>
          <select v-model="moveTo" :aria-label="$t('events.roles.moveTo')" class="h-9 rounded-lg border border-mnema-border bg-mnema-surface px-2 text-sm text-mnema-text max-md:h-11">
            <option value="" disabled>{{ $t('events.roles.moveTo') }}</option>
            <option v-for="t in targets" :key="t.key" :value="t.id">{{ t.name }}</option>
          </select>
          <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs max-md:min-h-11" :disabled="!moveTo" @click="confirmMove(row)">{{ $t('events.roles.removeRoleConfirm') }}</button>
          <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs max-md:min-h-11" @click="asking = null">{{ $t('common.cancel') }}</button>
        </div>
      </li>
    </ul>

    <div class="flex flex-wrap items-center gap-2">
      <button type="button" data-testid="event-role-add" :disabled="!canAdd" class="btn btn-secondary flex items-center gap-1.5 px-3 py-1.5 text-xs max-md:min-h-11" @click="add">
        <Plus class="h-3.5 w-3.5" />{{ $t('events.roles.addRole') }}
      </button>
      <select v-if="enabledTemplates.length" data-testid="event-role-template" :aria-label="$t('events.roles.fromTemplate')" class="h-8 rounded-lg border border-mnema-border bg-mnema-surface px-2 text-xs text-mnema-text max-md:h-11" @change="pickTemplate">
        <option value="">{{ $t('events.roles.fromTemplate') }}</option>
        <option v-for="t in enabledTemplates" :key="t.id" :value="t.id">{{ t.name }}</option>
      </select>
    </div>

    <EventRoleTemplates v-if="isAdmin" :templates="templates" :rows="rows" @changed="loadTemplates" />
  </div>
</template>
