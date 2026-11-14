<script setup lang="ts">
// The bounds for invites that members create (Create invites permission): the longest validity, the
// most uses of one invite, how many may be active per member and how many a member may make per
// hour. One draft with one Save (doc/settings-ux.md); the server enforces them (doc/invites.md).
import { onMounted, ref } from 'vue'
import { api } from '../../shared/lib/api'
import { decodeAuthMemberInviteLimits } from '../../types/rest'
import type { AuthMemberInviteLimits } from '../../types/rest'
import { useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'
import StatePanel from '../../shared/components/StatePanel.vue'

const LIMITS = '/api/admin/invites/limits'
const toasts = useToastStore()
const stored = ref<AuthMemberInviteLimits | null>(null)
const failed = ref(false)

type Form = { hours: number; uses: number; active: number; perHour: number }
const inRange = (n: number, max: number) => Number.isInteger(n) && n >= 1 && n <= max
const fromServer = (l: AuthMemberInviteLimits): Form => ({ hours: l.max_expires_in_hours, uses: l.max_uses, active: l.max_active, perHour: l.max_per_hour })

const form = useSettingsDraft<Form>({
  source: () => (stored.value ? fromServer(stored.value) : { hours: 168, uses: 25, active: 10, perHour: 10 }),
  validate: d => (inRange(d.hours, 8760) && inRange(d.uses, 1000) && inRange(d.active, 100) && inRange(d.perHour, 100) ? {} : { hours: t('memberInvites.limits.invalid') }),
  blocked: () => !stored.value,
  save: async ({ draft, commit }) => {
    const saved = await api(LIMITS, {
      method: 'PUT',
      json: { max_expires_in_hours: draft.hours, max_uses: draft.uses, max_active: draft.active, max_per_hour: draft.perHour },
      decode: decodeAuthMemberInviteLimits
    })
    stored.value = saved
    commit(['hours', 'uses', 'active', 'perHour'], fromServer(saved))
  },
  saved: () => { toasts.success(t('memberInvites.limits.saved')) }
})
const { draft, errors } = form

const fields = [
  { key: 'hours', label: 'memberInvites.limits.hours', max: 8760 },
  { key: 'uses', label: 'memberInvites.limits.uses', max: 1000 },
  { key: 'active', label: 'memberInvites.limits.active', max: 100 },
  { key: 'perHour', label: 'memberInvites.limits.perHour', max: 100 }
] as const

async function load() {
  failed.value = false
  try {
    stored.value = await api(LIMITS, { decode: decodeAuthMemberInviteLimits })
  } catch {
    failed.value = true
  }
}

onMounted(load)
</script>

<template>
  <section v-if="stored || failed" class="space-y-3 rounded-lg border border-mnema-hairline p-4" data-testid="member-invite-limits">
    <h4 class="text-sm font-semibold text-mnema-text">{{ $t('memberInvites.limits.title') }}</h4>
    <p class="text-xs text-mnema-tertiary">{{ $t('memberInvites.limits.hint') }}</p>
    <StatePanel v-if="!stored" state="error" retryable data-testid="member-invite-limits-error" @retry="load">{{ $t('memberInvites.limits.loadFailed') }}</StatePanel>
    <form v-else class="space-y-3" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="flex flex-wrap items-end gap-4">
        <div v-for="f in fields" :key="f.key" class="min-w-0 space-y-1">
          <label :for="`member-invite-${f.key}`" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t(f.label) }}</label>
          <input
            :id="`member-invite-${f.key}`"
            v-model.number="draft[f.key]"
            type="number"
            min="1"
            :max="f.max"
            step="1"
            inputmode="numeric"
            :aria-invalid="errors.hours ? 'true' : 'false'"
            class="w-28 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
            :data-testid="`member-invite-${f.key}`"
          />
        </div>
      </div>
      <p v-if="errors.hours" class="text-xs text-mnema-danger" role="alert">{{ errors.hours }}</p>
      <UnsavedChangesBar inline save-testid="member-invite-limits-save" :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
  </section>
</template>
