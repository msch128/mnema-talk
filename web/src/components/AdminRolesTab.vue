<script setup lang="ts">
// Admin dashboard: roles. A list ordered by rank (drag, or the arrows) and the
// editor of the selected role (RoleEditor, one draft per role). Ordering,
// creating and deleting are actions and run when used.
import { ref, computed, watch, onMounted } from 'vue'
import { Shield, Plus, GripVertical, ChevronUp, ChevronDown, AtSign, Lock } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { decodeRole } from '../types/domain'
import type { Role } from '../types/domain'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { roleColorCss } from '../lib/permissions'
import { t } from '../i18n'
import RoleEditor from './RoleEditor.vue'

const permissions = usePermissionsStore()
const toasts = useToastStore()

const selectedId = ref<string | null>(null)
const busy = ref(false)
const dragging = ref<string | null>(null)
// The editor of the open role holds its unsaved draft.
const editor = ref<{ guard: () => Promise<boolean>; dirty: boolean } | null>(null)

const everyone = computed(() => permissions.roles.find(r => r.is_everyone))
// Highest rank first, as the server lists them; @everyone is separate, and
// the managed roles of single bots stay out of the hierarchy people arrange.
const custom = computed(() => permissions.roles.filter(r => !r.is_everyone && !r.bot_user_id))
const botRoles = computed(() => permissions.roles.filter(r => !!r.bot_user_id))
const selected = computed(() => permissions.roles.find(r => r.id === selectedId.value) ?? null)

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

// Leaving the open role with unsaved edits asks first.
const mayDiscardEdits = async () => !editor.value?.dirty || editor.value.guard()

async function choose(id: string) {
  if (id === selectedId.value) return
  if (!(await mayDiscardEdits())) return
  selectedId.value = id
}

// A reload keeps the open role and its draft, unless the role is gone.
watch(() => permissions.roles, () => {
  if (selectedId.value && !permissions.roles.some(r => r.id === selectedId.value)) selectedId.value = null
})

async function create() {
  if (busy.value || !(await mayDiscardEdits())) return
  busy.value = true
  try {
    const role = await api('/api/admin/roles', { method: 'POST', json: { name: t('roles.newName') }, decode: decodeRole })
    await permissions.loadRoles()
    selectedId.value = role.id
    toasts.success(t('roles.created'))
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function remove(role: Role) {
  const ok = await confirm({ title: t('roles.deleteTitle', { name: role.name }), body: t('roles.deleteBody'), confirmLabel: t('common.delete'), danger: true })
  if (!ok) return
  try {
    await api(`/api/admin/roles/${role.id}`, { method: 'DELETE' })
    selectedId.value = null
    await permissions.loadRoles()
    toasts.success(t('roles.deleted'))
  } catch (e) {
    fail(e)
  }
}

// Swapping two roles moves both, so both must be below the member's own rank.
function canMove(index: number, delta: number) {
  const a = custom.value[index]
  const b = custom.value[index + delta]
  return !!a && !!b && permissions.outranks(a) && permissions.outranks(b)
}

async function commitOrder(ids: string[]) {
  try {
    await api('/api/admin/roles/order', { method: 'PUT', json: { role_ids: ids } })
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('roles.reorderFailed')))
  }
  await permissions.loadRoles()
}

async function move(index: number, delta: number) {
  if (!canMove(index, delta)) return
  const ids = custom.value.map(r => r.id)
  const [id] = ids.splice(index, 1)
  ids.splice(index + delta, 0, id as string)
  await commitOrder(ids)
}

function dragIgnored(from: string | null, targetId: string): boolean {
  if (!from) return true
  return from === targetId
}

function dropOutOfRange(fromIndex: number, toIndex: number): boolean {
  if (fromIndex < 0) return true
  return toIndex < 0
}

function dropRankBlocked(moved: Role | undefined, target: Role | undefined): boolean {
  if (!moved || !target) return true
  if (!permissions.outranks(moved)) return true
  return !permissions.outranks(target)
}

function onDrop(targetId: string) {
  const from = dragging.value
  dragging.value = null
  if (dragIgnored(from, targetId)) return
  if (!from) return
  const ids = custom.value.map(r => r.id)
  const fromIndex = ids.indexOf(from)
  const toIndex = ids.indexOf(targetId)
  if (dropOutOfRange(fromIndex, toIndex)) return
  if (dropRankBlocked(custom.value[fromIndex], custom.value[toIndex])) return
  ids.splice(fromIndex, 1)
  ids.splice(toIndex, 0, from)
  void commitOrder(ids)
}

onMounted(async () => {
  await permissions.loadRoles()
  if (!selectedId.value && everyone.value) selectedId.value = everyone.value.id
})
</script>

<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between gap-3">
      <h3 class="flex items-center gap-2 font-mono text-sm font-semibold uppercase tracking-wider text-mnema-text">
        <Shield class="h-4 w-4 text-mnema-accent" />
        <span>{{ $t('roles.title') }}</span>
      </h3>
      <button
        type="button"
        :disabled="busy"
        class="btn btn-primary flex items-center gap-1.5"
        @click="create"
      >
        <Plus class="h-4 w-4" />
        <span>{{ $t('roles.new') }}</span>
      </button>
    </div>
    <p class="text-sm text-mnema-tertiary">{{ $t('roles.intro') }}</p>

    <div class="grid gap-4 md:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
      <!-- Roles by rank -->
      <div>
        <ul :aria-label="$t('roles.list')" class="overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
          <li v-if="!custom.length" class="p-4 text-sm text-mnema-tertiary">{{ $t('roles.empty') }}</li>
          <li
            v-for="(role, index) in custom"
            :key="role.id"
            :data-role="role.name"
            :draggable="permissions.outranks(role)"
            :class="['flex items-center gap-1 border-b border-mnema-hairline last:border-b-0', selectedId === role.id ? 'bg-mnema-accent-subtle' : '']"
            @dragstart="dragging = role.id"
            @dragend="dragging = null"
            @dragover.prevent
            @drop.prevent="onDrop(role.id)"
          >
            <GripVertical class="ml-1 h-4 w-4 shrink-0 cursor-grab text-mnema-tertiary" :aria-label="$t('roles.drag', { name: role.name })" role="img" />
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 py-2 text-left text-sm" :aria-pressed="selectedId === role.id" @click="choose(role.id)">
              <span class="h-3 w-3 shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>
              <span class="truncate text-mnema-text">{{ role.name }}</span>
              <Lock v-if="role.managed" class="h-3 w-3 shrink-0 text-mnema-tertiary" role="img" v-tooltip="$t('roles.managedHint')" data-testid="role-managed-icon" />
            </button>
            <button type="button" class="p-1 text-mnema-tertiary transition hover:text-mnema-text disabled:opacity-30" :disabled="!canMove(index, -1)" :aria-label="$t('roles.moveUp', { name: role.name })" @click="move(index, -1)">
              <ChevronUp class="h-4 w-4" />
            </button>
            <button type="button" class="p-1 pr-2 text-mnema-tertiary transition hover:text-mnema-text disabled:opacity-30" :disabled="!canMove(index, 1)" :aria-label="$t('roles.moveDown', { name: role.name })" @click="move(index, 1)">
              <ChevronDown class="h-4 w-4" />
            </button>
          </li>
          <template v-if="botRoles.length">
            <li class="border-t border-mnema-hairline px-3 pb-1 pt-3 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary" data-testid="bot-roles-heading">{{ $t('roles.botRoles') }}</li>
            <li
              v-for="role in botRoles"
              :key="role.id"
              :data-role="role.name"
              data-bot-role
              :class="['flex items-center gap-1 border-t border-mnema-hairline', selectedId === role.id ? 'bg-mnema-accent-subtle' : '']"
            >
              <button type="button" class="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left text-sm" :aria-pressed="selectedId === role.id" @click="choose(role.id)">
                <span class="h-3 w-3 shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>
                <span class="truncate text-mnema-text">{{ role.name }}</span>
                <Lock class="h-3 w-3 shrink-0 text-mnema-tertiary" role="img" v-tooltip="$t('roles.botRoleHint')" data-testid="role-managed-icon" />
              </button>
            </li>
          </template>
          <li v-if="everyone" class="border-t border-mnema-hairline">
            <button type="button" class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm" :class="selectedId === everyone.id ? 'bg-mnema-accent-subtle' : ''" :aria-pressed="selectedId === everyone.id" @click="choose(everyone.id)">
              <AtSign class="h-3.5 w-3.5 shrink-0 text-mnema-tertiary" />
              <span class="truncate text-mnema-text">{{ $t('roles.everyone') }}</span>
            </button>
          </li>
        </ul>
        <p class="mt-2 text-xs text-mnema-tertiary">{{ $t('roles.reorderHint') }}</p>
      </div>

      <!-- Editor -->
      <RoleEditor v-if="selected" :key="selected.id" ref="editor" :role="selected" @remove="remove" />
    </div>
  </section>
</template>
