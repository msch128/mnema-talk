<script setup lang="ts">
// Owner and Administrators: raid protection. The lockdown shuts the door at
// once and is switched on and off with one click; the sign-up limit and the
// waiting times of new members are settings with an explicit Save
// (doc/settings-ux.md). The server enforces all of it (doc/moderation.md).
import { computed, onMounted, ref } from 'vue'
import { ShieldAlert } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useRaidStore } from '../stores/raid'
import { useToastStore } from '../stores/toast'
import { useSettingsDraft } from '../composables/useSettingsDraft'
import { useReloadOnActivate } from '../composables/useReloadOnActivate'
import { durationLabel } from '../lib/moderation'
import { withCurrent } from '../lib/automod'
import presets from '../lib/presets.json'
import { locale, t } from '../i18n'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const MAX_LIMIT = 1000
const MAX_WINDOW = 1440
const toasts = useToastStore()
const raid = useRaidStore()

interface RaidForm { limit: number; window: number; write: number; react: number; speak: number }
const WAITS = ['write', 'react', 'speak'] as const

const form = useSettingsDraft<RaidForm>({
  source: () => ({
    limit: raid.settings?.join_limit ?? 0, window: raid.settings?.join_window_minutes ?? 10,
    write: raid.settings?.min_age_write_minutes ?? 0, react: raid.settings?.min_age_react_minutes ?? 0, speak: raid.settings?.min_age_speak_minutes ?? 0
  }),
  validate: draft => (Number.isInteger(draft.limit) && draft.limit >= 0 && draft.limit <= MAX_LIMIT && Number.isInteger(draft.window) && draft.window >= 1 && draft.window <= MAX_WINDOW
    ? {}
    : { limit: t('raid.throttle.invalid') }),
  blocked: () => !raid.settings,
  save: async ({ draft }) => {
    await raid.save({
      join_limit: draft.limit, join_window_minutes: draft.window,
      min_age_write_minutes: draft.write, min_age_react_minutes: draft.react, min_age_speak_minutes: draft.speak
    })
  },
  saved: () => { toasts.success(t('raid.saved')) }
})
const { draft, errors } = form

const pauseInvites = ref(false)
const hours = ref(0)
const busy = ref(false)
const lockdown = computed(() => raid.lockdown)
const until = computed(() => {
  const at = lockdown.value?.until
  return at ? new Date(at).toLocaleTimeString(locale.value, { timeStyle: 'short' }) : ''
})

const waitLabel = (minutes: number) => (minutes === 0 ? t('raid.waits.none') : durationLabel(minutes * 60))

async function toggleLockdown(active: boolean) {
  busy.value = true
  try {
    await raid.setLockdown({ active, pause_invites: active && pauseInvites.value, hours: active ? hours.value : 0 })
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('raid.lockdown.failed')))
  } finally {
    busy.value = false
  }
}

useReloadOnActivate(() => raid.load())
onMounted(() => raid.load())
</script>

<template>
  <section class="space-y-6" data-testid="admin-raid">
    <div class="space-y-1">
      <h3 class="flex items-center gap-2 font-mono text-sm font-semibold uppercase tracking-wider text-mnema-text">
        <ShieldAlert class="h-4 w-4 text-mnema-accent" /><span>{{ $t('raid.title') }}</span>
      </h3>
      <p class="text-xs text-mnema-tertiary">{{ $t('raid.hint') }}</p>
    </div>

    <p v-if="raid.failed" role="alert" class="text-sm text-mnema-danger">{{ $t('raid.loadFailed') }}</p>
    <template v-else-if="raid.settings">
      <div class="space-y-3 rounded-lg border border-mnema-hairline bg-mnema-surface p-4" data-testid="lockdown-card">
        <h4 class="text-sm font-semibold text-mnema-text">{{ $t('raid.lockdown.title') }}</h4>
        <template v-if="lockdown?.active">
          <p class="text-sm text-mnema-text" role="status" data-testid="lockdown-state">{{ until ? $t('raid.lockdown.onUntil', { time: until }) : $t('raid.lockdown.onText') }}</p>
          <p v-if="lockdown.pause_invites" class="text-xs text-mnema-tertiary">{{ $t('raid.lockdown.invitesPaused') }}</p>
          <button type="button" class="btn btn-primary btn-sm" data-testid="lockdown-end" :disabled="busy" @click="toggleLockdown(false)">{{ $t('raid.lockdown.end') }}</button>
        </template>
        <template v-else>
          <p class="text-sm text-mnema-muted" data-testid="lockdown-state">{{ $t('raid.lockdown.offText') }}</p>
          <p class="text-xs text-mnema-tertiary">{{ $t('raid.lockdown.hint') }}</p>
          <label class="flex items-center gap-2 text-sm text-mnema-text">
            <input v-model="pauseInvites" type="checkbox" data-testid="lockdown-pause-invites" />
            <span>{{ $t('raid.lockdown.pauseInvites') }}</span>
          </label>
          <div class="flex flex-wrap items-center gap-2">
            <label for="lockdown-hours" class="text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('raid.lockdown.ends') }}</label>
            <select id="lockdown-hours" v-model.number="hours" class="rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text" data-testid="lockdown-hours">
              <option v-for="h in presets.lockdownHours" :key="h" :value="h">{{ $t(`raid.lockdown.durations.${h}`) }}</option>
            </select>
            <button type="button" class="btn btn-danger btn-sm" data-testid="lockdown-start" :disabled="busy" @click="toggleLockdown(true)">{{ $t('raid.lockdown.start') }}</button>
          </div>
        </template>
      </div>

      <form class="space-y-6" data-testid="raid-form" @submit.prevent="form.save" @keydown="form.onKeydown">
        <div class="space-y-2">
          <h4 class="text-sm font-semibold text-mnema-text">{{ $t('raid.throttle.title') }}</h4>
          <p class="text-xs text-mnema-tertiary">{{ $t('raid.throttle.hint') }}</p>
          <div class="flex flex-wrap items-end gap-4">
            <div class="space-y-1">
              <label for="raid-limit" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('raid.throttle.limit') }}</label>
              <input id="raid-limit" v-model.number="draft.limit" type="number" min="0" :max="MAX_LIMIT" step="1" inputmode="numeric" :aria-invalid="errors.limit ? 'true' : 'false'" class="w-28 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="raid-limit" />
            </div>
            <div class="space-y-1">
              <label for="raid-window" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('raid.throttle.window') }}</label>
              <input id="raid-window" v-model.number="draft.window" type="number" min="1" :max="MAX_WINDOW" step="1" inputmode="numeric" :aria-invalid="errors.limit ? 'true' : 'false'" class="w-28 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="raid-window" />
            </div>
          </div>
          <p class="text-xs text-mnema-tertiary" data-testid="raid-joined">{{ $t('raid.throttle.status', { count: raid.settings.joined_in_window, minutes: raid.settings.join_window_minutes }) }}</p>
          <p v-if="errors.limit" class="text-xs text-mnema-danger" role="alert">{{ errors.limit }}</p>
        </div>

        <div class="space-y-2">
          <h4 class="text-sm font-semibold text-mnema-text">{{ $t('raid.waits.title') }}</h4>
          <p class="text-xs text-mnema-tertiary">{{ $t('raid.waits.hint') }}</p>
          <p class="text-xs text-mnema-tertiary" data-testid="raid-wait-existing">{{ $t('raid.waits.existing') }}</p>
          <div class="flex flex-wrap gap-4">
            <div v-for="action in WAITS" :key="action" class="space-y-1">
              <label :for="`raid-wait-${action}`" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t(`raid.waits.${action}`) }}</label>
              <select :id="`raid-wait-${action}`" v-model.number="draft[action]" class="rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text" :data-testid="`raid-wait-${action}`">
                <option v-for="m in withCurrent(presets.newMemberWaitMinutes, draft[action])" :key="m" :value="m">{{ waitLabel(m) }}</option>
              </select>
            </div>
          </div>
        </div>

        <UnsavedChangesBar sticky save-testid="raid-save" :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
      </form>
    </template>
  </section>
</template>
