<script setup lang="ts">
// Host for moderate() from lib/moderation: the timeout and the ban dialog.
// A timeout takes a duration (ready-made or custom) and a reason; a ban takes
// the same, "forever" being the default, and says whether to revoke the invite
// the account registered with.
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import {
  pendingModeration, DURATION_CHOICES, CUSTOM_UNITS, customSeconds, durationLabel,
  MIN_TIMEOUT_SECONDS, MAX_TIMEOUT_SECONDS, MIN_BAN_SECONDS, MAX_BAN_SECONDS, MAX_REASON_LENGTH
} from '../lib/moderation'
import type { CustomUnit } from '../lib/moderation'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const request = computed(() => pendingModeration.value)
const requestKey = computed(() => Symbol(request.value?.target.id ?? 'moderation'))
const toasts = useToastStore()

const isBan = computed(() => request.value?.kind === 'ban')
// 'forever' only exists for bans; 'custom' reads the amount and unit below.
const choice = ref<string>('3600')
const amount = ref('')
const unit = ref<CustomUnit>('minutes')
const reason = ref('')
const revokeInvite = ref(true)
const error = ref('')
const saving = ref(false)

const seconds = computed<number | null>(() => {
  if (choice.value === 'forever') return null
  if (choice.value === 'custom') return customSeconds(amount.value, unit.value)
  const n = Number(choice.value)
  return Number.isInteger(n) && n > 0 ? n : null
})
const limits = computed(() => (isBan.value ? [MIN_BAN_SECONDS, MAX_BAN_SECONDS] : [MIN_TIMEOUT_SECONDS, MAX_TIMEOUT_SECONDS]) as [number, number])
const valid = computed(() => {
  if (choice.value === 'forever') return true
  const s = seconds.value
  return s !== null && s >= limits.value[0] && s <= limits.value[1]
})

// Every request starts from the same defaults: an hour, or a ban for good.
watch(request, req => {
  choice.value = req?.kind === 'ban' ? 'forever' : '3600'
  amount.value = ''
  unit.value = 'minutes'
  reason.value = ''
  revokeInvite.value = true
  error.value = ''
  saving.value = false
}, { immediate: true })

function close(done: boolean) {
  request.value?.resolve(done)
}

async function submit() {
  const req = request.value
  if (!req || !valid.value || saving.value) return
  error.value = ''
  saving.value = true
  try {
    const base = `/api/admin/users/${req.target.id}`
    if (req.kind === 'timeout') {
      await api(`${base}/timeout`, { method: 'POST', json: { duration_seconds: seconds.value, reason: reason.value.trim() } })
      toasts.success(t('moderation.timedOutToast', { name: req.target.name }))
    } else {
      await api(`${base}/ban`, {
        method: 'POST',
        json: { duration_seconds: seconds.value, reason: reason.value.trim(), revoke_invite: revokeInvite.value }
      })
      toasts.success(t('moderation.bannedToast', { name: req.target.name }))
    }
    close(true)
  } catch (e) {
    error.value = caughtErrorMessage(e, t('moderation.failed'))
  } finally {
    saving.value = false
  }
}

onBeforeUnmount(() => request.value?.resolve(false))

const inputClass = 'w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent'
const chipClass = (on: boolean) => [
  'h-8 rounded-md border px-3 text-sm font-medium transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-mnema-accent',
  on ? 'border-mnema-accent/40 bg-mnema-accent/15 text-mnema-accent' : 'border-mnema-border text-mnema-muted hover:text-mnema-text'
]
</script>

<template>
  <BaseDialog v-if="request" :key="requestKey" panel-class="max-w-[480px]" @close="close(false)">
    <template #default="{ titleId }">
      <form class="space-y-4 overflow-y-auto p-5" @submit.prevent="submit">
        <h2 :id="titleId" class="break-words text-lg font-semibold text-mnema-text">
          {{ isBan ? $t('moderation.banTitle', { name: request.target.name }) : $t('moderation.timeoutTitle', { name: request.target.name }) }}
        </h2>
        <p class="text-sm text-mnema-muted">{{ isBan ? $t('moderation.banBody') : $t('moderation.timeoutBody') }}</p>

        <div v-if="error" role="alert" class="rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</div>

        <fieldset class="space-y-2">
          <legend class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('moderation.duration') }}</legend>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="d in DURATION_CHOICES"
              :key="d"
              type="button"
              :data-testid="`duration-${d}`"
              :aria-pressed="choice === String(d) ? 'true' : 'false'"
              :class="chipClass(choice === String(d))"
              @click="choice = String(d)"
            >{{ durationLabel(d) }}</button>
            <button
              v-if="isBan"
              type="button"
              data-testid="duration-forever"
              :aria-pressed="choice === 'forever' ? 'true' : 'false'"
              :class="chipClass(choice === 'forever')"
              @click="choice = 'forever'"
            >{{ $t('moderation.forever') }}</button>
            <button
              type="button"
              data-testid="duration-custom"
              :aria-pressed="choice === 'custom' ? 'true' : 'false'"
              :class="chipClass(choice === 'custom')"
              @click="choice = 'custom'"
            >{{ $t('moderation.custom') }}</button>
          </div>
          <div v-if="choice === 'custom'" class="flex gap-2">
            <input
              v-model="amount"
              type="number"
              min="1"
              inputmode="numeric"
              data-testid="custom-amount"
              :aria-label="$t('moderation.customAmount')"
              :class="inputClass"
            />
            <select v-model="unit" data-testid="custom-unit" :aria-label="$t('moderation.customUnit')" :class="inputClass">
              <option v-for="(_, u) in CUSTOM_UNITS" :key="u" :value="u">{{ $t(`moderation.unitNames.${u}`) }}</option>
            </select>
          </div>
          <p v-if="choice === 'custom' && amount && !valid" class="text-xs text-mnema-danger">
            {{ $t('moderation.durationRange', { min: durationLabel(limits[0]), max: durationLabel(limits[1]) }) }}
          </p>
        </fieldset>

        <div class="space-y-1.5">
          <label for="moderation-reason" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('moderation.reason') }}</label>
          <input
            id="moderation-reason"
            v-model="reason"
            type="text"
            data-testid="reason"
            :maxlength="MAX_REASON_LENGTH"
            :placeholder="$t('moderation.reasonPlaceholder')"
            :class="inputClass"
          />
        </div>

        <label v-if="isBan" class="flex items-center gap-2 text-sm text-mnema-text">
          <input v-model="revokeInvite" type="checkbox" data-testid="revoke-invite" class="h-4 w-4" />
          <span>{{ $t('moderation.revokeInvite') }}</span>
        </label>

        <div class="flex items-center justify-end gap-2 border-t border-mnema-hairline pt-3">
          <button
            type="button"
            class="h-8 rounded-md border border-mnema-border px-3 text-sm font-semibold text-mnema-text transition hover:bg-mnema-hover"
            @click="close(false)"
          >{{ $t('common.cancel') }}</button>
          <button
            type="submit"
            data-testid="moderation-submit"
            :disabled="!valid || saving"
            class="h-8 rounded-md bg-mnema-danger px-3 text-sm font-semibold text-mnema-accent-ink transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-30"
          >{{ isBan ? $t('moderation.banConfirm') : $t('moderation.timeoutConfirm') }}</button>
        </div>
      </form>
    </template>
  </BaseDialog>
</template>
