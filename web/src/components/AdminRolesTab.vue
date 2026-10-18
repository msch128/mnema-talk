<script setup lang="ts">
// Admin dashboard: roles. A list ordered by rank (drag, or the arrows) and an
// editor for the selected role: name, colour, permissions. The server checks
// every rule (rank, granting only what you hold); this only offers what fits.
import { ref, computed, watch, onMounted } from 'vue'
import { Shield, Plus, GripVertical, ChevronUp, ChevronDown, Trash2, AtSign, Lock } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { decodeRole } from '../types/domain'
import type { Role } from '../types/domain'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { PERMISSION_BITS, PERMISSION_KEYS, ROLE_COLOR_PRESETS, parseRoleColor, roleColorCss } from '../lib/permissions'
import { t } from '../i18n'
import PermissionToggles from './PermissionToggles.vue'

const permissions = usePermissionsStore()
const toasts = useToastStore()

const selectedId = ref<string | null>(null)
const name = ref('')
const color = ref(0)
const hex = ref('')
const hoist = ref(false)
// Upload limit override in MB as typed; '' inherits the global limit.
const uploadMb = ref<string | number>('')
const allow = ref(0)
const busy = ref(false)
const dragging = ref<string | null>(null)

const everyone = computed(() => permissions.roles.find(r => r.is_everyone))
// Highest rank first, as the server lists them; @everyone is separate.
const custom = computed(() => permissions.roles.filter(r => !r.is_everyone))
const selected = computed(() => permissions.roles.find(r => r.id === selectedId.value) ?? null)
const isEveryone = computed(() => !!selected.value?.is_everyone)
// The built-in Bot role: every bot holds it. It keeps its name and cannot be deleted.
const isManaged = computed(() => !!selected.value?.managed)
// Roles at or above the member's own rank can be looked at, not changed.
const readonly = computed(() => !!selected.value && !isEveryone.value && !permissions.outranks(selected.value))
// Permissions the member does not hold cannot be handed out.
const lockedBits = computed(() => {
  if (permissions.isOwner) return 0
  const all = Object.values(PERMISSION_BITS).reduce((a, b) => a | b, 0)
  return all & ~(permissions.info?.permissions ?? 0)
})
const uploadValue = computed<number | null | undefined>(() => {
  const raw = String(uploadMb.value ?? '').trim()
  if (raw === '') return null
  const n = Number(raw)
  return Number.isInteger(n) && n >= 1 ? n : undefined
})
function isDirty(role: Role) {
  return allow.value !== role.permissions
    || (permissions.isOwner && uploadValue.value !== undefined && uploadValue.value !== role.max_upload_mb)
    || (!role.is_everyone && (name.value.trim() !== role.name || color.value !== role.color || hoist.value !== role.hoist))
}

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

function select(id: string | null) {
  selectedId.value = id
  const role = permissions.roles.find(r => r.id === id)
  name.value = role?.name ?? ''
  color.value = role?.color ?? 0
  hex.value = roleColorCss(color.value)
  hoist.value = role?.hoist ?? false
  allow.value = role?.permissions ?? 0
  uploadMb.value = role?.max_upload_mb == null ? '' : String(role.max_upload_mb)
}

// A reload keeps the open role and its draft, unless the role is gone.
watch(() => permissions.roles, () => {
  if (selectedId.value && !permissions.roles.some(r => r.id === selectedId.value)) select(null)
})

function pickColor(value: number) {
  color.value = value
  hex.value = roleColorCss(value)
}
function typeColor() {
  const parsed = parseRoleColor(hex.value)
  if (parsed !== null) color.value = parsed
}

async function create() {
  busy.value = true
  try {
    const role = await api('/api/admin/roles', { method: 'POST', json: { name: t('roles.newName') }, decode: decodeRole })
    await permissions.loadRoles()
    select(role.id)
    toasts.success(t('roles.created'))
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function save() {
  const role = selected.value
  if (!role || busy.value) return
  const trimmed = name.value.trim()
  if (!role.is_everyone && !trimmed) {
    toasts.error(t('roles.nameRequired'))
    return
  }
  busy.value = true
  try {
    if (permissions.isOwner && uploadValue.value === undefined) {
      toasts.error(t('roles.uploadInvalid'))
      busy.value = false
      return
    }
    // Only the Owner may send the upload limit; it is the one field the
    // server refuses from role managers.
    const upload = permissions.isOwner && uploadValue.value !== role.max_upload_mb ? { max_upload_mb: uploadValue.value } : {}
    const json = role.is_everyone
      ? { permissions: allow.value, ...upload }
      : { name: trimmed, color: color.value, hoist: hoist.value, permissions: allow.value, ...upload }
    await api(`/api/admin/roles/${role.id}`, { method: 'PATCH', json, decode: decodeRole })
    await permissions.loadRoles()
    select(role.id)
    toasts.success(t('roles.saved'))
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
    select(null)
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

function onDrop(targetId: string) {
  const from = dragging.value
  dragging.value = null
  if (!from || from === targetId) return
  const ids = custom.value.map(r => r.id)
  const fromIndex = ids.indexOf(from)
  const toIndex = ids.indexOf(targetId)
  const moved = custom.value[fromIndex]
  const target = custom.value[toIndex]
  if (fromIndex < 0 || toIndex < 0 || !moved || !target || !permissions.outranks(moved) || !permissions.outranks(target)) return
  ids.splice(fromIndex, 1)
  ids.splice(toIndex, 0, from)
  void commitOrder(ids)
}

onMounted(async () => {
  await permissions.loadRoles()
  if (!selectedId.value && everyone.value) select(everyone.value.id)
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
        class="flex items-center gap-1.5 rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink shadow-sm transition hover:bg-mnema-accent-hover disabled:opacity-50"
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
            @dragover.prevent
            @drop.prevent="onDrop(role.id)"
          >
            <GripVertical class="ml-1 h-4 w-4 flex-shrink-0 cursor-grab text-mnema-tertiary" :aria-label="$t('roles.drag', { name: role.name })" role="img" />
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 py-2 text-left text-sm" :aria-pressed="selectedId === role.id" @click="select(role.id)">
              <span class="h-3 w-3 flex-shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>
              <span class="truncate text-mnema-text">{{ role.name }}</span>
              <Lock v-if="role.managed" class="h-3 w-3 flex-shrink-0 text-mnema-tertiary" role="img" :aria-label="$t('roles.managedHint')" data-testid="role-managed-icon" />
            </button>
            <button type="button" class="p-1 text-mnema-tertiary transition hover:text-mnema-text disabled:opacity-30" :disabled="!canMove(index, -1)" :aria-label="$t('roles.moveUp', { name: role.name })" @click="move(index, -1)">
              <ChevronUp class="h-4 w-4" />
            </button>
            <button type="button" class="p-1 pr-2 text-mnema-tertiary transition hover:text-mnema-text disabled:opacity-30" :disabled="!canMove(index, 1)" :aria-label="$t('roles.moveDown', { name: role.name })" @click="move(index, 1)">
              <ChevronDown class="h-4 w-4" />
            </button>
          </li>
          <li v-if="everyone" class="border-t border-mnema-hairline">
            <button type="button" class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm" :class="selectedId === everyone.id ? 'bg-mnema-accent-subtle' : ''" :aria-pressed="selectedId === everyone.id" @click="select(everyone.id)">
              <AtSign class="h-3.5 w-3.5 flex-shrink-0 text-mnema-tertiary" />
              <span class="truncate text-mnema-text">{{ $t('roles.everyone') }}</span>
            </button>
          </li>
        </ul>
        <p class="mt-2 text-xs text-mnema-tertiary">{{ $t('roles.reorderHint') }}</p>
      </div>

      <!-- Editor -->
      <div v-if="selected" class="min-w-0 space-y-4" :aria-label="$t('roles.selected')" role="group">
        <p v-if="isEveryone" class="text-sm text-mnema-tertiary">{{ $t('roles.everyoneHint') }}</p>
        <p v-else-if="isManaged" class="flex items-start gap-2 rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-tertiary" data-testid="role-managed-note">
          <Lock class="mt-0.5 h-4 w-4 flex-shrink-0" aria-hidden="true" />
          <span>{{ $t('roles.managedNote') }}</span>
        </p>
        <p v-if="!isEveryone && readonly" class="rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-tertiary">{{ $t('roles.locked') }}</p>

        <template v-if="!isEveryone">
          <label class="block space-y-1">
            <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.name') }}</span>
            <input v-model="name" maxlength="32" :disabled="readonly || isManaged" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-none transition focus:border-mnema-accent disabled:opacity-50" />
          </label>

          <div class="space-y-1">
            <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.color') }}</span>
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" :disabled="readonly" :aria-pressed="color === 0" :aria-label="$t('roles.colorNone')" class="h-6 w-6 rounded-full border border-mnema-border-strong text-xs text-mnema-tertiary disabled:opacity-50" :class="color === 0 ? 'ring-2 ring-mnema-accent' : ''" @click="pickColor(0)">&times;</button>
              <button
                v-for="preset in ROLE_COLOR_PRESETS"
                :key="preset"
                type="button"
                :disabled="readonly"
                :aria-pressed="color === preset"
                :aria-label="roleColorCss(preset)"
                class="h-6 w-6 rounded-full border border-mnema-border-strong disabled:opacity-50"
                :class="color === preset ? 'ring-2 ring-mnema-accent' : ''"
                :style="{ background: roleColorCss(preset) }"
                @click="pickColor(preset)"
              ></button>
              <input v-model="hex" :disabled="readonly" :aria-label="$t('roles.colorCustom')" :placeholder="'#rrggbb'" maxlength="7" class="w-24 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 font-mono text-xs text-mnema-text outline-none focus:border-mnema-accent disabled:opacity-50" @input="typeColor" />
            </div>
          </div>

          <label class="flex items-center gap-2 text-sm text-mnema-text">
            <input v-model="hoist" type="checkbox" :disabled="readonly" class="accent-mnema-accent" />
            <span>{{ $t('roles.hoist') }}</span>
          </label>
        </template>

        <label v-if="permissions.isOwner && !isManaged" class="block space-y-1">
          <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.uploadLimit') }}</span>
          <span class="flex items-center gap-2">
            <input v-model="uploadMb" type="number" inputmode="numeric" min="1" step="1" :placeholder="$t('roles.uploadInherit')" :aria-label="$t('roles.uploadLimit')" class="w-32 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm tabular-nums text-mnema-text outline-none focus:border-mnema-accent" />
            <span class="text-sm text-mnema-tertiary">MB</span>
          </span>
          <span class="block text-xs text-mnema-tertiary">{{ $t('roles.uploadHint') }}</span>
        </label>

        <div class="space-y-2">
          <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.permissions') }}</span>
          <PermissionToggles v-model:allow="allow" :keys="PERMISSION_KEYS" :locked="lockedBits" :readonly="readonly" />
          <p v-if="lockedBits && !readonly" class="text-xs text-mnema-tertiary">{{ $t('roles.cannotGrant') }}</p>
        </div>

        <div v-if="!readonly" class="flex items-center justify-between gap-3">
          <button type="button" :disabled="!isDirty(selected) || busy" class="rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink shadow-sm transition hover:bg-mnema-accent-hover disabled:opacity-40" @click="save">{{ $t('roles.save') }}</button>
          <button v-if="!isEveryone && !isManaged" type="button" class="flex items-center gap-1.5 text-sm text-mnema-tertiary transition hover:text-mnema-danger" @click="remove(selected)">
            <Trash2 class="h-4 w-4" />
            <span>{{ $t('roles.delete') }}</span>
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
