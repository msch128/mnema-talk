<script setup lang="ts">
// The editor of one role: name, colour, hoist, upload limit and permissions are
// one draft with one Save (doc/settings-ux.md). The parent keys it by role, so
// every role gets its own draft; deleting the role is an action. The server
// checks every rule (rank, granting only what you hold); this only offers what fits.
import type { PropType } from 'vue'
import { computed, ref } from 'vue'
import { Trash2, Lock, Eye } from '@lucide/vue'
import { api } from '../lib/api'
import { decodeRole } from '../types/domain'
import type { Role } from '../types/domain'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { uploadPatch } from '../lib/roleSave'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { PERMISSION_BITS, ROLE_COLOR_PRESETS, blockedForSelfAssign, parseRoleColor, roleColorCss } from '../lib/permissions'
import { t } from '../i18n'
import RolePermissionPicker from './RolePermissionPicker.vue'
import RoleViewAsDialog from './RoleViewAsDialog.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const props = defineProps({
  role: { type: Object as PropType<Role>, required: true }
})
const emit = defineEmits<{ remove: [role: Role] }>()

const permissions = usePermissionsStore()
const toasts = useToastStore()

const isEveryone = computed(() => props.role.is_everyone)
// The shared Bot role (every bot holds it) and the bot-owned roles: they keep
// their names and cannot be deleted.
const isManaged = computed(() => !!props.role.managed)
const isBotRole = computed(() => !!props.role.bot_user_id)
// Roles at or above the member's own rank can be looked at, not changed.
const readonly = computed(() => !isEveryone.value && !permissions.outranks(props.role))
// Permissions the member does not hold cannot be handed out.
const lockedBits = computed(() => {
  if (permissions.isOwner) return 0
  const all = Object.values(PERMISSION_BITS).reduce((a, b) => a | b, 0)
  return all & ~(permissions.info?.permissions ?? 0)
})
const displayName = computed(() => (isEveryone.value ? t('roles.everyone') : props.role.name))
// The read-only "view server as this role" preview is open.
const viewingAs = ref(false)

interface RoleForm {
  name: string
  color: number
  /** The colour as typed; only the colour it stands for counts. */
  hex: string
  hoist: boolean
  /** Upload limit override in MB as typed; '' inherits the global limit. */
  upload: string | number
  allow: number
  /** Members may take and drop the role themselves. */
  selfAssignable: boolean
  description: string
}

// The limit as the server takes it: null inherits, a whole number of MB, or undefined for text it would ignore.
function uploadOf(raw: string | number): number | null | undefined {
  const text = String(raw ?? '').trim()
  if (text === '') return null
  const n = Number(text)
  return Number.isInteger(n) && n >= 1 ? n : undefined
}

// The flag and the description go along only when they were edited.
function selfAssign(draft: RoleForm, changed: (keyof RoleForm)[]) {
  return {
    ...(changed.includes('selfAssignable') ? { self_assignable: draft.selfAssignable } : {}),
    ...(changed.includes('description') ? { description: normalise.text(draft.description) } : {})
  }
}

const form = useSettingsDraft<RoleForm>({
  source: () => ({
    name: props.role.name,
    color: props.role.color,
    hex: roleColorCss(props.role.color),
    hoist: props.role.hoist,
    upload: props.role.max_upload_mb == null ? '' : String(props.role.max_upload_mb),
    allow: props.role.permissions,
    selfAssignable: props.role.self_assignable ?? false,
    description: props.role.description ?? ''
  }),
  normalisers: {
    // @everyone has only its permissions (and, for the Owner, the limit).
    name: value => (isEveryone.value ? '' : normalise.text(value)),
    color: value => (isEveryone.value ? 0 : value),
    // Typed text counts as the colour it means; an empty box is "no colour", unreadable text is its own value.
    hex: value => parseRoleColor(value) ?? (value.trim() === '' ? 0 : `invalid:${value.trim()}`),
    hoist: value => (isEveryone.value ? false : value),
    selfAssignable: value => !isEveryone.value && !isManaged.value && value,
    description: value => (isEveryone.value ? '' : normalise.text(value)),
    // Only the Owner may send the limit.
    upload: value => {
      const limit = uploadOf(value)
      return permissions.isOwner ? (limit === undefined ? `invalid:${String(value).trim()}` : limit) : null
    }
  },
  validate: draft => {
    const found: Partial<Record<keyof RoleForm, string>> = {}
    if (!isEveryone.value && !normalise.text(draft.name)) found.name = t('roles.nameRequired')
    if (permissions.isOwner && uploadOf(draft.upload) === undefined) found.upload = t('roles.uploadInvalid')
    if (draft.hex.trim() !== '' && parseRoleColor(draft.hex) === null) found.hex = t('roles.colorInvalid')
    if (draft.selfAssignable && blockedForSelfAssign(draft.allow)) found.selfAssignable = t('roles.selfAssign.blocked')
    return found
  },
  blocked: () => readonly.value,
  save: async ({ draft, changed, commit }) => {
    const upload = uploadPatch(permissions.isOwner, uploadOf(draft.upload), props.role.max_upload_mb)
    const json = isEveryone.value
      ? { permissions: draft.allow, ...upload }
      : { name: normalise.text(draft.name), color: draft.color, hoist: draft.hoist, permissions: draft.allow, ...selfAssign(draft, changed), ...upload }
    await api(`/api/admin/roles/${props.role.id}`, { method: 'PATCH', json, decode: decodeRole })
    commit(changed)
  },
  saved: async () => {
    await permissions.loadRoles()
    toasts.success(t('roles.saved'))
  }
})
const { draft, errors } = form

function pickColor(value: number) {
  draft.color = value
  draft.hex = roleColorCss(value)
}
function typeColor() {
  // An emptied box means "no colour".
  if (draft.hex.trim() === '') draft.color = 0
  const parsed = parseRoleColor(draft.hex)
  if (parsed !== null) draft.color = parsed
}

defineExpose({ guard: form.guard, dirty: form.dirty })
</script>

<template>
  <div class="min-w-0 space-y-4" :aria-label="$t('roles.selected')" role="group" @keydown="form.onKeydown">
    <div class="flex items-center justify-between gap-3">
      <h4 class="min-w-0 truncate text-base font-semibold text-mnema-text" data-testid="role-editor-title">{{ displayName }}</h4>
      <button type="button" class="btn btn-secondary btn-sm shrink-0" data-testid="role-view-as" @click="viewingAs = true"><Eye class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('roles.viewAs.open', { name: displayName }) }}</button>
    </div>
    <p v-if="isEveryone" class="text-sm text-mnema-tertiary">{{ $t('roles.everyoneHint') }}</p>
    <p v-else-if="isManaged" class="flex items-start gap-2 rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-tertiary" data-testid="role-managed-note">
      <Lock class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
      <span>{{ isBotRole ? $t('roles.botRoleNote', { name: role.name }) : $t('roles.managedNote') }}</span>
    </p>
    <p v-if="!isEveryone && readonly" class="rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-tertiary">{{ $t('roles.locked') }}</p>

    <template v-if="!isEveryone">
      <label class="block stack-1">
        <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('common.name') }}</span>
        <input v-model="draft.name" v-max-chars="32" :disabled="readonly || isManaged" :aria-invalid="errors.name ? 'true' : undefined" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-hidden transition focus:border-mnema-accent disabled:opacity-50" />
        <span v-if="errors.name" class="block text-xs text-mnema-danger" role="alert" data-testid="role-name-invalid">{{ errors.name }}</span>
      </label>

      <div class="stack-1">
        <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.color') }}</span>
        <div class="flex flex-wrap items-center gap-2">
          <button type="button" :disabled="readonly" :aria-pressed="draft.color === 0" :aria-label="$t('roles.colorNone')" class="h-6 w-6 rounded-full border border-mnema-border-strong text-xs text-mnema-tertiary disabled:opacity-50" :class="draft.color === 0 ? 'ring-2 ring-mnema-accent' : ''" @click="pickColor(0)">&times;</button>
          <button
            v-for="preset in ROLE_COLOR_PRESETS"
            :key="preset"
            type="button"
            :disabled="readonly"
            :aria-pressed="draft.color === preset"
            :aria-label="roleColorCss(preset)"
            class="h-6 w-6 rounded-full border border-mnema-border-strong disabled:opacity-50"
            :class="draft.color === preset ? 'ring-2 ring-mnema-accent' : ''"
            :style="{ background: roleColorCss(preset) }"
            @click="pickColor(preset)"
          ></button>
          <input v-model="draft.hex" :disabled="readonly" :aria-label="$t('roles.colorCustom')" :aria-invalid="!!errors.hex" :placeholder="'#rrggbb'" maxlength="7" class="w-24 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 font-mono text-xs text-mnema-text outline-hidden focus:border-mnema-accent disabled:opacity-50" @input="typeColor" />
        </div>
        <p v-if="errors.hex" class="text-xs text-mnema-danger" role="alert" data-testid="role-color-invalid">{{ errors.hex }}</p>
      </div>

      <label class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="draft.hoist" type="checkbox" :disabled="readonly" />
        <span>{{ $t('roles.hoist') }}</span>
      </label>
    </template>

    <div v-if="!isEveryone" class="stack-3" data-testid="role-self-assign">
      <label class="block stack-1">
        <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.selfAssign.description') }}</span>
        <input v-model="draft.description" v-max-chars="200" :disabled="readonly" :placeholder="$t('roles.selfAssign.descriptionPlaceholder')" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-hidden transition focus:border-mnema-accent disabled:opacity-50" data-testid="role-description" />
      </label>
      <label v-if="!isManaged" class="flex items-start gap-2 text-sm text-mnema-text">
        <input v-model="draft.selfAssignable" type="checkbox" class="mt-1" :disabled="readonly || (!draft.selfAssignable && blockedForSelfAssign(draft.allow))" data-testid="role-self-assignable" />
        <span>
          <span class="block">{{ $t('roles.selfAssign.label') }}</span>
          <span class="block text-xs text-mnema-tertiary">{{ $t('roles.selfAssign.hint') }}</span>
          <span v-if="errors.selfAssignable" class="block text-xs text-mnema-danger" role="alert" data-testid="role-self-assign-blocked">{{ errors.selfAssignable }}</span>
          <span v-else-if="!draft.selfAssignable && blockedForSelfAssign(draft.allow)" class="block text-xs text-mnema-tertiary">{{ $t('roles.selfAssign.unavailable') }}</span>
        </span>
      </label>
    </div>

    <label v-if="permissions.isOwner && !isManaged" class="block stack-1">
      <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.uploadLimit') }}</span>
      <span class="flex items-center gap-2">
        <input v-model="draft.upload" type="number" inputmode="numeric" min="1" step="1" :placeholder="$t('roles.uploadInherit')" :aria-label="$t('roles.uploadLimit')" :aria-invalid="!!errors.upload" class="w-32 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm tabular-nums text-mnema-text outline-hidden focus:border-mnema-accent" />
        <span class="text-sm text-mnema-tertiary">MB</span>
      </span>
      <span v-if="errors.upload" class="block text-xs text-mnema-danger" role="alert" data-testid="role-upload-invalid">{{ errors.upload }}</span>
      <span v-else class="block text-xs text-mnema-tertiary">{{ $t('roles.uploadHint') }}</span>
    </label>

    <RolePermissionPicker v-model:allow="draft.allow" :locked="lockedBits" :readonly="readonly" />

    <RoleViewAsDialog v-if="viewingAs" :role-id="role.id" :role-name="displayName" @close="viewingAs = false" />

    <div v-if="!readonly" class="sticky bottom-0 -mb-6 bg-mnema-elevated max-md:-mb-4" data-testid="role-actions">
      <div v-if="!isEveryone && !isManaged" class="border-t border-mnema-hairline py-2">
        <button type="button" class="flex items-center gap-1.5 text-sm text-mnema-tertiary transition hover:text-mnema-danger" data-testid="role-delete" @click="emit('remove', role)">
          <Trash2 class="h-4 w-4" />
          <span>{{ $t('roles.delete') }}</span>
        </button>
      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </div>
  </div>
</template>
