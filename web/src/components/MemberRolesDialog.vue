<script setup lang="ts">
// Choose the roles of one member. Roles at or above the signed-in member's
// own rank, or with permissions they lack, stay as they are; the server
// enforces the same rules.
import type { PropType } from 'vue'
import { computed } from 'vue'
import { api } from '../lib/api'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { PERMISSION_BITS, roleColorCss } from '../lib/permissions'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const props = defineProps({
  user: { type: Object as PropType<{ id: string; username: string; display_name: string; role: string }>, required: true }
})
const emit = defineEmits<{ close: []; saved: [] }>()

const permissions = usePermissionsStore()
const auth = useAuthStore()
const toasts = useToastStore()

const isOwnerTarget = computed(() => props.user.role === 'admin')
const isSelf = computed(() => auth.user?.id === props.user.id)
// The managed Bot role belongs to bots; it is never offered for people.
const roles = computed(() => permissions.roles.filter(r => !r.is_everyone && !r.managed))
const heldBits = computed(() => (permissions.isOwner ? Object.values(PERMISSION_BITS).reduce((a, b) => a | b, 0) : (permissions.info?.permissions ?? 0)))
const frozen = computed(() => isOwnerTarget.value || isSelf.value || !permissions.outranksMember(props.user.id))

// A role can be given or taken only below the member's own rank and with
// permissions they hold themselves.
function locked(roleId: string) {
  const role = permissions.roleById(roleId)
  if (!role || frozen.value) return true
  return !permissions.outranks(role) || (role.permissions & ~heldBits.value) !== 0
}

// One draft with one Save (doc/settings-ux.md): the roles are stored when Save is pressed.
const form = useSettingsDraft<{ roles: string[] }>({
  source: () => ({ roles: [...(permissions.memberRoles[props.user.id] ?? [])] }),
  normalisers: { roles: normalise.set },
  blocked: () => frozen.value,
  save: async ({ draft }) => {
    await api(`/api/admin/users/${props.user.id}/roles`, { method: 'PUT', json: { role_ids: [...draft.roles] } })
  },
  saved: () => {
    toasts.success(t('memberRoles.saved'))
    emit('saved')
    emit('close')
  }
})
const { draft } = form

function toggle(roleId: string) {
  draft.roles = draft.roles.includes(roleId) ? draft.roles.filter(id => id !== roleId) : [...draft.roles, roleId]
}

</script>

<template>
  <BaseDialog :title="$t('memberRoles.title', { name: user.display_name || user.username })" panel-class="max-w-md" :guard="form.guard" @close="emit('close')">
    <div class="flex min-h-0 flex-1 flex-col" data-testid="member-roles-form" @keydown="form.onKeydown">
    <div class="space-y-3 overflow-y-auto p-5">
      <p class="text-sm text-mnema-tertiary">
        {{ isOwnerTarget ? $t('memberRoles.owner') : isSelf ? $t('memberRoles.own') : $t('memberRoles.hint') }}
      </p>
      <p v-if="!roles.length" class="text-sm text-mnema-tertiary">{{ $t('roles.empty') }}</p>
      <ul v-else class="divide-y divide-mnema-hairline rounded-lg border border-mnema-hairline bg-mnema-surface">
        <li v-for="role in roles" :key="role.id" :data-role="role.name">
          <label class="flex cursor-pointer items-center gap-3 px-4 py-2.5 text-sm" :class="locked(role.id) ? 'cursor-not-allowed opacity-60' : ''">
            <input type="checkbox" :checked="draft.roles.includes(role.id)" :disabled="locked(role.id)" @change="toggle(role.id)" />
            <span class="h-3 w-3 shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>
            <span class="min-w-0 flex-1 truncate text-mnema-text">{{ role.name }}</span>
            <span v-if="locked(role.id) && !frozen" class="shrink-0 text-xs text-mnema-tertiary">{{ $t('memberRoles.locked') }}</span>
          </label>
        </li>
      </ul>
    </div>
    <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </div>
  </BaseDialog>
</template>
