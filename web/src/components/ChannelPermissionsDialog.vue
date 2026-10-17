<script setup lang="ts">
// Who may do what in one channel or category: allow/deny entries for roles
// and single members. A channel also inherits its category (its own entries
// win); making a channel private is denying "View channels" to @everyone and
// allowing it for the roles that belong. The server checks rank and what the
// editor holds themselves; this only offers what fits.
import type { PropType } from 'vue'
import { ref, computed, onMounted } from 'vue'
import { Trash2, Lock } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeOverwritesBody } from '../types/domain'
import type { Overwrite } from '../types/domain'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { CHANNEL_PERMISSION_KEYS, PERMISSION_BITS } from '../lib/permissions'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import PermissionToggles from './PermissionToggles.vue'

const props = defineProps({
  kind: { type: String as PropType<'channel' | 'category'>, required: true },
  target: { type: Object as PropType<{ id: string; name: string }>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const permissions = usePermissionsStore()
const chat = useChatStore()
const toasts = useToastStore()

const entries = ref<Overwrite[]>([])
const selected = ref<string | null>(null)
const loaded = ref(false)
const failed = ref(false)
const busy = ref(false)

const base = computed(() => `/api/admin/${props.kind === 'channel' ? 'channels' : 'categories'}/${props.target.id}/overwrites`)
const key = (o: { type: string; id: string }) => `${o.type}:${o.id}`
const current = computed(() => entries.value.find(o => key(o) === selected.value) ?? null)

const everyone = computed(() => permissions.roles.find(r => r.is_everyone))

function nameOf(o: Overwrite): string {
  if (o.type === 'role') return permissions.roleById(o.id)?.name ?? o.id
  const member = chat.members.find(m => m.id === o.id)
  return member?.display_name || member?.username || o.id
}

// A role entry is editable for @everyone and roles below the editor's rank; a
// member entry for members below it.
function editable(o: Overwrite): boolean {
  if (o.type === 'member') return permissions.outranksMember(o.id)
  const role = permissions.roleById(o.id)
  return !!role && (role.is_everyone || permissions.outranks(role))
}
const readonly = computed(() => !!current.value && !editable(current.value))

// Permissions the editor does not hold in this place cannot be allowed or denied.
const lockedBits = computed(() => {
  let bits = 0
  for (const permissionKey of CHANNEL_PERMISSION_KEYS) {
    const held = props.kind === 'channel' ? permissions.canIn(props.target.id, permissionKey) : permissions.can(permissionKey)
    if (!held) bits |= PERMISSION_BITS[permissionKey]
  }
  return bits
})

const addableRoles = computed(() => permissions.roles.filter(r => (r.is_everyone || permissions.outranks(r)) && !entries.value.some(o => o.type === 'role' && o.id === r.id)))
const addableMembers = computed(() => chat.members.filter(m => m.role !== 'admin' && permissions.outranksMember(m.id) && !entries.value.some(o => o.type === 'member' && o.id === m.id)))

function add(type: 'role' | 'member', id: string) {
  if (!id) return
  const entry: Overwrite = { type, id, allow: 0, deny: 0 }
  entries.value = [...entries.value, entry]
  selected.value = key(entry)
}

function update(field: 'allow' | 'deny', value: number) {
  const o = current.value
  if (!o) return
  entries.value = entries.value.map(e => (key(e) === key(o) ? { ...e, [field]: value } : e))
}

function remove(o: Overwrite) {
  entries.value = entries.value.filter(e => key(e) !== key(o))
  if (selected.value === key(o)) selected.value = null
}

// Deny viewing for @everyone: the first step to a private channel.
function makePrivate() {
  const role = everyone.value
  if (!role) return
  const view = PERMISSION_BITS.view_channel
  const existing = entries.value.find(o => o.type === 'role' && o.id === role.id)
  if (existing) {
    entries.value = entries.value.map(e => (e === existing ? { ...e, allow: e.allow & ~view, deny: e.deny | view } : e))
  } else {
    entries.value = [...entries.value, { type: 'role', id: role.id, allow: 0, deny: view }]
  }
  selected.value = `role:${role.id}`
}

async function load() {
  try {
    const body = await api(base.value, { decode: decodeOverwritesBody })
    entries.value = body.overwrites
    selected.value = entries.value[0] ? key(entries.value[0]) : null
  } catch (e) {
    failed.value = true
    toasts.error(caughtErrorMessage(e, t('overwrites.loadFailed')))
  } finally {
    loaded.value = true
  }
}

async function save() {
  busy.value = true
  try {
    const overwrites = entries.value.filter(o => o.allow !== 0 || o.deny !== 0)
    await api(base.value, { method: 'PUT', json: { overwrites } })
    toasts.success(t('overwrites.saved'))
    emit('close')
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <BaseDialog
    :title="kind === 'channel' ? $t('overwrites.titleChannel', { name: target.name }) : $t('overwrites.titleCategory', { name: target.name })"
    panel-class="max-w-3xl max-h-[90vh]"
    @close="emit('close')"
  >
    <div class="flex-1 space-y-4 overflow-y-auto p-5">
      <p class="text-sm text-mnema-tertiary">{{ kind === 'channel' ? $t('overwrites.hintChannel') : $t('overwrites.hintCategory') }}</p>
      <p class="text-xs text-mnema-tertiary">{{ $t('overwrites.ownerNote') }}</p>

      <div v-if="loaded && !failed" class="grid gap-4 md:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
        <div class="space-y-3">
          <ul :aria-label="$t('overwrites.subjects')" class="overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
            <li v-if="!entries.length" class="p-3 text-sm text-mnema-tertiary">{{ $t('overwrites.empty') }}</li>
            <li v-for="o in entries" :key="key(o)" :data-entry="key(o)" :class="['flex items-center border-b border-mnema-hairline last:border-b-0', selected === key(o) ? 'bg-mnema-accent-subtle' : '']">
              <button type="button" class="min-w-0 flex-1 px-3 py-2 text-left text-sm" :aria-pressed="selected === key(o)" @click="selected = key(o)">
                <span class="block truncate text-mnema-text">{{ nameOf(o) }}</span>
                <span class="text-xs text-mnema-tertiary">{{ o.type === 'role' ? $t('overwrites.role') : $t('overwrites.member') }}</span>
              </button>
              <button v-if="editable(o)" type="button" class="p-2 text-mnema-tertiary transition hover:text-mnema-danger" :aria-label="$t('overwrites.remove', { name: nameOf(o) })" @click="remove(o)">
                <Trash2 class="h-4 w-4" />
              </button>
            </li>
          </ul>

          <select class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1.5 text-sm text-mnema-text" :aria-label="$t('overwrites.addRole')" @change="add('role', ($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).value = ''">
            <option value="">{{ $t('overwrites.addRole') }}</option>
            <option v-for="role in addableRoles" :key="role.id" :value="role.id">{{ role.name }}</option>
          </select>
          <select class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1.5 text-sm text-mnema-text" :aria-label="$t('overwrites.addMember')" @change="add('member', ($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).value = ''">
            <option value="">{{ $t('overwrites.addMember') }}</option>
            <option v-for="m in addableMembers" :key="m.id" :value="m.id">{{ m.display_name || m.username }}</option>
          </select>
          <button v-if="everyone && permissions.can('view_channel')" type="button" class="flex w-full items-center justify-center gap-2 rounded-md border border-mnema-border-strong px-3 py-1.5 text-sm text-mnema-text transition hover:bg-mnema-hover" :title="$t('overwrites.privateHint')" @click="makePrivate">
            <Lock class="h-4 w-4" />
            <span>{{ $t('overwrites.private') }}</span>
          </button>
        </div>

        <div class="min-w-0">
          <PermissionToggles
            v-if="current"
            mode="tri"
            :keys="CHANNEL_PERMISSION_KEYS"
            :allow="current.allow"
            :deny="current.deny"
            :locked="lockedBits"
            :readonly="readonly"
            @update:allow="update('allow', $event)"
            @update:deny="update('deny', $event)"
          />
        </div>
      </div>

      <div class="flex justify-end gap-2">
        <button type="button" class="rounded-md px-3.5 py-1.5 text-sm text-mnema-muted transition hover:text-mnema-text" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="button" :disabled="busy || !loaded || failed" class="rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40" @click="save">{{ $t('overwrites.save') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>
