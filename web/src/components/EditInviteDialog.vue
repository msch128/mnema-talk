<script setup lang="ts">
import type { PropType } from 'vue'
// Change the expiry and the use limit of an existing invite. Only what was
// changed is sent: null removes the expiry or the limit, a field left alone
// is kept. One draft, one Save (doc/settings-ux.md).
import { api } from '../lib/api'
import type { Invite, UpdateInviteRequest } from '../types/domain'
import { decodeAuthInvite } from '../types/rest'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import type { FieldErrors } from '../composables/useSettingsDraft'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const props = defineProps({
  invite: { type: Object as PropType<Invite>, required: true }
})
const emit = defineEmits<{ close: []; saved: [invite: Invite] }>()

const toasts = useToastStore()

// Server bounds: 1 to 1000 uses, at most 8760 hours ahead.
const MAX_USES = 1000
const MAX_HOURS = 8760
const EXPIRY_PRESETS: readonly { key: string; minutes: number }[] = [
  { key: 'min30', minutes: 30 }, { key: 'hour1', minutes: 60 }, { key: 'hour6', minutes: 360 },
  { key: 'hour12', minutes: 720 }, { key: 'day', minutes: 1440 }, { key: 'week', minutes: 10080 }
]
const USES_PRESETS = [1, 5, 10, 25, 50, 100] as const

// expiry: 'keep' leaves the expiry alone, 'never' removes it, a preset key sets it
// from now, 'custom' uses the date field.
// uses: 'keep', 'unlimited', a preset number as text, or 'custom'.
interface InviteForm { expiry: string; customDate: string; uses: string; customUses: string }

function localInput(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// The new expiry as the server wants it; undefined when it stays as it is,
// false when the input is not acceptable.
function expiryPatch(f: InviteForm): string | null | undefined | false {
  if (f.expiry === 'keep') return undefined
  if (f.expiry === 'never') return props.invite.expires_at ? null : undefined
  let at: number
  if (f.expiry === 'custom') {
    at = new Date(f.customDate).getTime()
    if (Number.isNaN(at) || at <= Date.now() || at > Date.now() + MAX_HOURS * 3600000) return false
  } else {
    const preset = EXPIRY_PRESETS.find(p => p.key === f.expiry)
    at = Date.now() + (preset?.minutes ?? 0) * 60000
  }
  return new Date(at).toISOString()
}

function usesPatch(f: InviteForm): number | null | undefined | false {
  if (f.uses === 'keep') return undefined
  if (f.uses === 'unlimited') return props.invite.max_uses == null ? undefined : null
  const n = f.uses === 'custom' ? Number(f.customUses) : Number(f.uses)
  if (!Number.isInteger(n) || n < Math.max(1, props.invite.uses_count) || n > MAX_USES) return false
  return n === props.invite.max_uses ? undefined : n
}

// Choices that amount to "leave it as it is" compare equal to the saved state.
const effectiveExpiry = (f: InviteForm) => (expiryPatch(f) === undefined ? 'keep' : f.expiry === 'custom' ? `custom:${f.customDate}` : f.expiry)
const effectiveUses = (f: InviteForm) => (usesPatch(f) === undefined ? 'keep' : f.uses === 'custom' ? `custom:${normalise.count(f.customUses)}` : f.uses)

const form = useSettingsDraft<InviteForm>({
  source: () => ({
    expiry: props.invite.expires_at ? 'keep' : 'never',
    customDate: localInput(props.invite.expires_at),
    uses: props.invite.max_uses == null ? 'unlimited' : 'keep',
    customUses: props.invite.max_uses == null ? '' : String(props.invite.max_uses)
  }),
  normalisers: {
    expiry: (_, state) => effectiveExpiry(state),
    customDate: () => '',
    uses: (_, state) => effectiveUses(state),
    customUses: () => ''
  },
  validate: draft => {
    const found: FieldErrors<InviteForm> = {}
    if (expiryPatch(draft) === false) found.expiry = t('admin.editInvite.expiryInvalid')
    if (usesPatch(draft) === false) found.uses = t('admin.editInvite.usesInvalid', { used: props.invite.uses_count, max: MAX_USES })
    return found
  },
  save: async ({ draft }) => {
    const body: UpdateInviteRequest = {}
    const e = expiryPatch(draft)
    const u = usesPatch(draft)
    if (e !== undefined && e !== false) body.expires_at = e
    if (u !== undefined && u !== false) body.max_uses = u
    const updated = await api(`/api/admin/invites/${props.invite.id}`, { method: 'PATCH', json: body, decode: decodeAuthInvite })
    toasts.success(t('admin.editInvite.saved'))
    emit('saved', updated)
    emit('close')
  }
})
const { draft, errors } = form

function pickExpiry(key: string) {
  draft.expiry = key
  if (key === 'custom' && !draft.customDate) draft.customDate = localInput(new Date(Date.now() + 86400000).toISOString())
}

const labelClass = 'text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono'
const fieldClass = 'w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden focus:border-mnema-accent'
</script>

<template>
  <BaseDialog :title="$t('admin.editInvite.title', { code: invite.code })" panel-class="max-w-md" :guard="form.guard" @close="emit('close')">
    <form class="flex min-h-0 flex-1 flex-col" data-testid="edit-invite-form" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="min-h-0 flex-1 space-y-5 overflow-y-auto p-6">

      <div class="stack-1.5">
        <span :class="labelClass">{{ $t('admin.editInvite.expiry') }}</span>
        <div class="flex flex-wrap gap-1.5" role="group" :aria-label="$t('admin.editInvite.expiry')">
          <button v-if="invite.expires_at" type="button" class="chip" data-expiry="keep" :aria-pressed="draft.expiry === 'keep' ? 'true' : 'false'" @click="pickExpiry('keep')">{{ $t('admin.editInvite.keep') }}</button>
          <button type="button" class="chip" data-expiry="never" :aria-pressed="draft.expiry === 'never' ? 'true' : 'false'" @click="pickExpiry('never')">{{ $t('admin.preset.never') }}</button>
          <button v-for="p in EXPIRY_PRESETS" :key="p.key" type="button" class="chip" :data-expiry="p.key" :aria-pressed="draft.expiry === p.key ? 'true' : 'false'" @click="pickExpiry(p.key)">{{ $t(`admin.editInvite.expiryPreset.${p.key}`) }}</button>
          <button type="button" class="chip" data-expiry="custom" :aria-pressed="draft.expiry === 'custom' ? 'true' : 'false'" @click="pickExpiry('custom')">{{ $t('admin.editInvite.custom') }}</button>
        </div>
        <p v-if="errors.expiry" role="alert" class="text-xs text-mnema-danger" data-testid="edit-invite-expiry-error">{{ errors.expiry }}</p>
        <input v-if="draft.expiry === 'custom'" v-model="draft.customDate" type="datetime-local" data-testid="edit-invite-date" :aria-label="$t('admin.editInvite.customDate')" :aria-invalid="errors.expiry ? 'true' : 'false'" :class="fieldClass" />
      </div>

      <div class="stack-1.5">
        <span :class="labelClass">{{ $t('admin.maxUsesLabel') }}</span>
        <div class="flex flex-wrap gap-1.5" role="group" :aria-label="$t('admin.maxUsesLabel')">
          <button v-if="invite.max_uses != null" type="button" class="chip" data-uses="keep" :aria-pressed="draft.uses === 'keep' ? 'true' : 'false'" @click="draft.uses = 'keep'">{{ $t('admin.editInvite.keep') }}</button>
          <button type="button" class="chip" data-uses="unlimited" :aria-pressed="draft.uses === 'unlimited' ? 'true' : 'false'" @click="draft.uses = 'unlimited'">{{ $t('admin.maxUsesPlaceholder') }}</button>
          <button v-for="n in USES_PRESETS" :key="n" type="button" class="chip" :data-uses="n" :aria-pressed="draft.uses === String(n) ? 'true' : 'false'" @click="draft.uses = String(n)">{{ n }}</button>
          <button type="button" class="chip" data-uses="custom" :aria-pressed="draft.uses === 'custom' ? 'true' : 'false'" @click="draft.uses = 'custom'">{{ $t('admin.editInvite.custom') }}</button>
        </div>
        <input v-if="draft.uses === 'custom'" v-model="draft.customUses" type="number" :min="Math.max(1, invite.uses_count)" :max="MAX_USES" data-testid="edit-invite-uses" :aria-label="$t('admin.editInvite.customUses')" :aria-invalid="errors.uses ? 'true' : 'false'" :class="fieldClass" />
        <p v-if="errors.uses" role="alert" class="text-xs text-mnema-danger" data-testid="edit-invite-uses-error">{{ errors.uses }}</p>
        <p class="text-xs text-mnema-tertiary">{{ $t('admin.editInvite.usesHint', { used: invite.uses_count }) }}</p>
      </div>

      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
  </BaseDialog>
</template>
