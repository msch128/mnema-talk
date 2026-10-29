<script setup lang="ts">
import type { PropType } from 'vue'
// Change the expiry and the use limit of an existing invite. Only what was
// changed is sent: null removes the expiry or the limit, a field left alone
// is kept.
import { computed, ref } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { Invite, UpdateInviteRequest } from '../types/domain'
import { decodeAuthInvite } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

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

// 'keep' leaves the expiry alone, 'never' removes it, a preset key sets it
// from now, 'custom' uses the date field.
const expiry = ref<string>(props.invite.expires_at ? 'keep' : 'never')
const customDate = ref(localInput(props.invite.expires_at))
// 'keep', 'unlimited', a preset number as text, or 'custom'.
const uses = ref<string>(props.invite.max_uses == null ? 'unlimited' : 'keep')
const customUses = ref(props.invite.max_uses == null ? '' : String(props.invite.max_uses))
const error = ref('')
const saving = ref(false)

function localInput(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function pickExpiry(key: string) {
  expiry.value = key
  if (key === 'custom' && !customDate.value) customDate.value = localInput(new Date(Date.now() + 86400000).toISOString())
}

// The new expiry as the server wants it; undefined when it stays as it is,
// false when the input is not acceptable.
function expiryPatch(): string | null | undefined | false {
  if (expiry.value === 'keep') return undefined
  if (expiry.value === 'never') return props.invite.expires_at ? null : undefined
  let at: number
  if (expiry.value === 'custom') {
    at = new Date(customDate.value).getTime()
    if (Number.isNaN(at) || at <= Date.now() || at > Date.now() + MAX_HOURS * 3600000) return false
  } else {
    const preset = EXPIRY_PRESETS.find(p => p.key === expiry.value)
    at = Date.now() + (preset?.minutes ?? 0) * 60000
  }
  return new Date(at).toISOString()
}

function usesPatch(): number | null | undefined | false {
  if (uses.value === 'keep') return undefined
  if (uses.value === 'unlimited') return props.invite.max_uses == null ? undefined : null
  const n = uses.value === 'custom' ? Number(customUses.value) : Number(uses.value)
  if (!Number.isInteger(n) || n < Math.max(1, props.invite.uses_count) || n > MAX_USES) return false
  return n === props.invite.max_uses ? undefined : n
}

const expiryInvalid = computed(() => expiry.value === 'custom' && expiryPatch() === false)
const usesInvalid = computed(() => usesPatch() === false)
const dirty = computed(() => expiryPatch() !== undefined || usesPatch() !== undefined)

async function submit() {
  const e = expiryPatch()
  const u = usesPatch()
  if (e === false) { error.value = t('admin.editInvite.expiryInvalid'); return }
  if (u === false) { error.value = t('admin.editInvite.usesInvalid', { used: props.invite.uses_count, max: MAX_USES }); return }
  const body: UpdateInviteRequest = {}
  if (e !== undefined) body.expires_at = e
  if (u !== undefined) body.max_uses = u
  error.value = ''
  saving.value = true
  try {
    const updated = await api(`/api/admin/invites/${props.invite.id}`, { method: 'PATCH', json: body, decode: decodeAuthInvite })
    toasts.success(t('admin.editInvite.saved'))
    emit('saved', updated)
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('admin.unknownError'))
  } finally {
    saving.value = false
  }
}

const labelClass = 'text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono'
const fieldClass = 'w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden focus:border-mnema-accent'
</script>

<template>
  <BaseDialog :title="$t('admin.editInvite.title', { code: invite.code })" panel-class="max-w-md" @close="emit('close')">
    <form class="p-5 space-y-5 overflow-y-auto" data-testid="edit-invite-form" @submit.prevent="submit">
      <div v-if="error" role="alert" class="p-2.5 rounded-lg bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm">{{ error }}</div>

      <div class="stack-1.5">
        <span :class="labelClass">{{ $t('admin.editInvite.expiry') }}</span>
        <div class="flex flex-wrap gap-1.5" role="group" :aria-label="$t('admin.editInvite.expiry')">
          <button v-if="invite.expires_at" type="button" class="chip" data-expiry="keep" :aria-pressed="expiry === 'keep' ? 'true' : 'false'" @click="pickExpiry('keep')">{{ $t('admin.editInvite.keep') }}</button>
          <button type="button" class="chip" data-expiry="never" :aria-pressed="expiry === 'never' ? 'true' : 'false'" @click="pickExpiry('never')">{{ $t('admin.preset.never') }}</button>
          <button v-for="p in EXPIRY_PRESETS" :key="p.key" type="button" class="chip" :data-expiry="p.key" :aria-pressed="expiry === p.key ? 'true' : 'false'" @click="pickExpiry(p.key)">{{ $t(`admin.editInvite.expiryPreset.${p.key}`) }}</button>
          <button type="button" class="chip" data-expiry="custom" :aria-pressed="expiry === 'custom' ? 'true' : 'false'" @click="pickExpiry('custom')">{{ $t('admin.editInvite.custom') }}</button>
        </div>
        <input v-if="expiry === 'custom'" v-model="customDate" type="datetime-local" data-testid="edit-invite-date" :aria-label="$t('admin.editInvite.customDate')" :aria-invalid="expiryInvalid ? 'true' : 'false'" :class="fieldClass" />
      </div>

      <div class="stack-1.5">
        <span :class="labelClass">{{ $t('admin.maxUsesLabel') }}</span>
        <div class="flex flex-wrap gap-1.5" role="group" :aria-label="$t('admin.maxUsesLabel')">
          <button v-if="invite.max_uses != null" type="button" class="chip" data-uses="keep" :aria-pressed="uses === 'keep' ? 'true' : 'false'" @click="uses = 'keep'">{{ $t('admin.editInvite.keep') }}</button>
          <button type="button" class="chip" data-uses="unlimited" :aria-pressed="uses === 'unlimited' ? 'true' : 'false'" @click="uses = 'unlimited'">{{ $t('admin.maxUsesPlaceholder') }}</button>
          <button v-for="n in USES_PRESETS" :key="n" type="button" class="chip" :data-uses="n" :aria-pressed="uses === String(n) ? 'true' : 'false'" @click="uses = String(n)">{{ n }}</button>
          <button type="button" class="chip" data-uses="custom" :aria-pressed="uses === 'custom' ? 'true' : 'false'" @click="uses = 'custom'">{{ $t('admin.editInvite.custom') }}</button>
        </div>
        <input v-if="uses === 'custom'" v-model="customUses" type="number" :min="Math.max(1, invite.uses_count)" :max="MAX_USES" data-testid="edit-invite-uses" :aria-label="$t('admin.editInvite.customUses')" :aria-invalid="usesInvalid ? 'true' : 'false'" :class="fieldClass" />
        <p class="text-xs text-mnema-tertiary">{{ $t('admin.editInvite.usesHint', { used: invite.uses_count }) }}</p>
      </div>

      <div class="pt-3 flex items-center justify-end gap-2 border-t border-mnema-hairline">
        <button type="button" class="btn btn-secondary h-8" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="saving || !dirty" class="btn btn-primary h-8 disabled:cursor-not-allowed">{{ $t('common.save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
