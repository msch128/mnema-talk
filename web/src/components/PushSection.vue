<script setup lang="ts">
import { computed } from 'vue'
import { Trash2 } from '@lucide/vue'
import { usePushSettings } from '../composables/usePushSettings'
import { locale } from '../i18n'

// Push notifications: turn them on for this device, send a test, choose
// whether they carry the message text, and see or remove the devices.
const push = usePushSettings()
const { availability, config, devices, mine, loading, busy, failed, blocked, enabledByOperator } = push

const preview = computed(() => config.value?.preview === true)
const previewLocked = computed(() => config.value?.preview_allowed === false)

function created(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium' }).format(new Date(value))
}

</script>

<template>
  <section class="space-y-2" data-testid="push-section">
    <h3 class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">{{ $t('push.title') }}</h3>
    <div class="rounded-lg bg-mnema-surface border border-mnema-hairline p-3.5 space-y-3">
      <p v-if="loading" class="text-xs text-mnema-tertiary" data-testid="push-loading">{{ $t('push.loading') }}</p>
      <p v-else-if="failed" class="text-xs text-mnema-danger" role="alert" data-testid="push-failed">{{ $t('push.loadFailed') }}</p>
      <p v-else-if="!enabledByOperator" class="text-xs text-mnema-tertiary" data-testid="push-off">{{ $t('push.offByOperator') }}</p>
      <p v-else-if="availability === 'needs-install'" class="text-xs text-mnema-tertiary" data-testid="push-needs-install">{{ $t('push.needsInstall') }}</p>
      <p v-else-if="availability === 'unsupported'" class="text-xs text-mnema-tertiary" data-testid="push-unsupported">{{ $t('push.unsupported') }}</p>
      <template v-else>
        <p class="text-xs text-mnema-tertiary leading-relaxed" data-testid="push-state">
          {{ mine ? $t('push.stateOn') : blocked ? $t('push.stateBlocked') : $t('push.stateOff') }}
          {{ $t('push.channelLevels') }}
        </p>
        <div class="flex flex-wrap items-center gap-2">
          <button
            v-if="!mine"
            type="button"
            data-testid="push-enable"
            :disabled="busy || blocked"
            class="btn btn-primary"
            @click="push.enable"
          >{{ $t('push.enable') }}</button>
          <template v-else>
            <button
              type="button"
              data-testid="push-test"
              :disabled="busy"
              class="btn btn-primary"
              @click="push.test"
            >{{ $t('push.test') }}</button>
            <button
              type="button"
              data-testid="push-disable"
              :disabled="busy"
              class="btn btn-danger"
              @click="push.remove(mine)"
            >{{ $t('push.disable') }}</button>
          </template>
        </div>

        <div class="flex items-start justify-between gap-4 border-t border-mnema-hairline pt-3">
          <div class="space-y-0.5">
            <div class="text-sm font-semibold text-mnema-text">{{ $t('push.previewTitle') }}</div>
            <p class="text-xs text-mnema-tertiary leading-relaxed" data-testid="push-preview-hint">
              {{ previewLocked ? $t('push.previewLocked') : $t('push.previewHint') }}
            </p>
          </div>
          <button
            type="button"
            role="switch"
            data-testid="push-preview"
            :aria-checked="preview && !previewLocked ? 'true' : 'false'"
            :aria-label="$t('push.previewTitle')"
            :disabled="busy || previewLocked"
            class="switch mt-1"
            @click="push.setText(!preview)"
          >
          </button>
        </div>
      </template>

      <div v-if="enabledByOperator && devices.length" class="border-t border-mnema-hairline pt-3 space-y-1.5" data-testid="push-devices">
        <div class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">{{ $t('push.devices') }}</div>
        <ul class="space-y-1">
          <li v-for="device in devices" :key="device.id" class="flex items-center gap-2 text-sm text-mnema-text" data-testid="push-device">
            <span class="min-w-0 flex-1 truncate">{{ device.label || $t('push.unnamedDevice') }}</span>
            <span v-if="mine && mine.id === device.id" class="rounded-md bg-mnema-accent/15 px-1.5 py-0.5 text-xs font-semibold text-mnema-accent">{{ $t('push.thisDevice') }}</span>
            <span class="text-xs text-mnema-tertiary">{{ created(device.created_at) }}</span>
            <button
              type="button"
              data-testid="push-remove"
              :disabled="busy"
              :aria-label="$t('push.remove', { name: device.label || $t('push.unnamedDevice') })"
              class="btn btn-icon btn-sm hover:text-mnema-danger"
              @click="push.remove(device)"
            >
              <Trash2 class="h-4 w-4" />
            </button>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>
