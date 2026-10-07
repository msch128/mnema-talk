<script setup lang="ts">
// Admin dashboard: version, health and load of the server (GET /api/admin/system).
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { RefreshCw, CheckCircle2, XCircle } from '@lucide/vue'
import { api, caughtErrorMessage, isApiError } from '../lib/api'
import type { SystemStatus } from '../types/domain'
import { decodeServerSystemStatus, decodeServerUpdateStatus, decodeServerHealth, type ServerSelfUpdateStarted } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { useAppVersionStore } from '../stores/appVersion'
import { t, locale } from '../i18n'
import SelfUpdateDialog from './SelfUpdateDialog.vue'

const toasts = useToastStore()
const versionStore = useAppVersionStore()

const status = ref<SystemStatus | null>(null)
const loading = ref(false)

const checking = ref(false)

async function refresh() {
  loading.value = true
  try {
    status.value = await api('/api/admin/system', { decode: decodeServerSystemStatus })
    versionStore.setAdminUpdate(status.value?.update)
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    loading.value = false
  }
}

async function checkNow() {
  checking.value = true
  try {
    const upd = await api('/api/admin/system/check', { method: 'POST', decode: decodeServerUpdateStatus })
    if (status.value) status.value = { ...status.value, update: upd }
    versionStore.setAdminUpdate(upd)
    if (upd?.check_error) toasts.error(t('admin.system.checkFailed', { reason: upd.check_error }))
    else toasts.success(upd?.update_available ? t('admin.system.updateFound', { version: upd.latest_version }) : t('admin.system.upToDate'))
  } catch (e) {
    toasts.error(isApiError(e) && e.code === 'RATE_LIMITED' ? t('admin.system.checkTooSoon') : caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    checking.value = false
  }
}

const UPDATE_COMMAND = 'docker compose pull app && docker compose up -d app'

async function copyCommand() {
  try {
    await navigator.clipboard.writeText(UPDATE_COMMAND)
    toasts.success(t('admin.system.copied'))
  } catch {
    toasts.error(t('admin.system.copyFailed'))
  }
}

function formatDate(iso: string | null | undefined) {
  if (!iso) return '–'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '–'
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(d)
}

// ---- Self-update (optional updater sidecar) ----
const self = computed(() => status.value?.self_update || null)
const showConfirm = ref(false)
// 'idle' | 'running' (waiting for the new version) | 'stalled' (no new version after a while)
const updatePhase = ref<'idle' | 'running' | 'stalled'>('idle')
const POLL_MS = 5000
const STALL_MS = 5 * 60 * 1000
let pollTimer: ReturnType<typeof setTimeout> | null = null
let pollStarted = 0

function stopPolling() {
  if (pollTimer !== null) clearTimeout(pollTimer)
  pollTimer = null
}

// /api/health is public and answers 503 or nothing while the container
// restarts; once it reports another version, the reload banner takes over.
async function pollHealth(fromVersion: string | undefined) {
  try {
    const res = await fetch('/api/health', { cache: 'no-store', credentials: 'same-origin' })
    if (res.ok) {
      const raw: unknown = await res.json()
      const data = decodeServerHealth(raw)
      if (data?.version && data.version !== fromVersion) {
        versionStore.setServerVersion(data.version)
        updatePhase.value = 'idle'
        stopPolling()
        toasts.success(t('admin.system.selfUpdateDone', { version: data.version }))
        return
      }
    }
  } catch {
    // Restarting: keep waiting.
  }
  if (Date.now() - pollStarted > STALL_MS) {
    updatePhase.value = 'stalled'
    stopPolling()
    return
  }
  pollTimer = setTimeout(() => pollHealth(fromVersion), POLL_MS)
}

function onUpdateStarted(res: ServerSelfUpdateStarted) {
  showConfirm.value = false
  updatePhase.value = 'running'
  pollStarted = Date.now()
  stopPolling()
  pollTimer = setTimeout(() => pollHealth(res?.from_version || status.value?.version?.current), POLL_MS)
}

onBeforeUnmount(stopPolling)

defineExpose({ refresh, status })

const health = computed(() => status.value?.health || null)
const upd = computed(() => status.value?.update || null)

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)))
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

function formatUptime(seconds: number) {
  const s = Math.max(0, Math.floor(seconds || 0))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return t('admin.system.uptimeDays', { d, h })
  if (h > 0) return t('admin.system.uptimeHours', { h, m })
  return t('admin.system.uptimeMinutes', { m })
}

onMounted(refresh)
</script>

<template>
  <section class="space-y-5" data-testid="admin-system">
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider">{{ $t('admin.system.heading') }}</h3>
      <button
        type="button"
        class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition disabled:opacity-40"
        :disabled="loading"
        :title="$t('admin.refresh')"
        :aria-label="$t('admin.refresh')"
        @click="refresh"
      >
        <RefreshCw class="w-4 h-4" :class="loading && 'animate-spin'" />
      </button>
    </div>

    <!-- Version -->
    <div v-if="status" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-2" data-testid="system-version">
      <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.system.version') }}</span>
      <div class="text-xl font-bold text-mnema-text font-mono">{{ status.version.current }}</div>
      <dl class="grid grid-cols-1 sm:grid-cols-3 gap-2 text-sm">
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.system.revision') }}</dt>
          <dd class="font-mono text-mnema-text">{{ status.version.revision || '–' }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.system.webVersion') }}</dt>
          <dd class="font-mono text-mnema-text">{{ versionStore.clientVersion }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.system.goVersion') }}</dt>
          <dd class="font-mono text-mnema-text">{{ status.version.go_version }}</dd>
        </div>
      </dl>
    </div>

    <!-- Updates -->
    <div v-if="upd" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="system-update">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="space-y-1">
          <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.system.updates') }}</span>
          <p v-if="!upd.check_enabled" class="text-sm text-mnema-muted" data-testid="update-check-disabled">{{ $t('admin.system.checkDisabled') }}</p>
          <template v-else>
            <p v-if="upd.update_available" class="text-sm font-semibold text-mnema-accent" data-testid="update-available">
              {{ $t('admin.system.updateAvailable', { current: upd.current_version, latest: upd.latest_version }) }}
            </p>
            <p v-else-if="upd.latest_version" class="text-sm text-mnema-text" data-testid="update-current">
              {{ $t('admin.system.latestIs', { latest: upd.latest_version }) }}
            </p>
            <p v-else class="text-sm text-mnema-muted">{{ $t('admin.system.notCheckedYet') }}</p>
            <p class="text-xs text-mnema-tertiary">{{ $t('admin.system.checkedAt', { when: formatDate(upd.checked_at) }) }}</p>
            <p v-if="upd.check_error" class="text-xs text-mnema-warning" data-testid="update-check-error">{{ $t('admin.system.checkFailed', { reason: upd.check_error }) }}</p>
          </template>
        </div>
        <button
          v-if="upd.check_enabled"
          type="button"
          data-testid="update-check-now"
          :disabled="checking"
          class="border border-mnema-border bg-mnema-canvas hover:bg-mnema-elevated text-mnema-text font-medium px-3 py-1.5 rounded-md text-sm transition disabled:opacity-40"
          @click="checkNow"
        >
          {{ checking ? $t('admin.system.checking') : $t('admin.system.checkNow') }}
        </button>
      </div>

      <!-- Self-update through the updater sidecar -->
      <div v-if="updatePhase === 'running'" role="status" class="rounded-md border border-mnema-accent/30 bg-mnema-accent/10 p-3 text-sm text-mnema-text" data-testid="self-update-running">
        {{ $t('admin.system.selfUpdateRunning') }}
      </div>
      <div v-else-if="updatePhase === 'stalled'" role="status" class="rounded-md border border-mnema-warning/35 bg-mnema-warning/10 p-3 text-sm text-mnema-text" data-testid="self-update-stalled">
        {{ $t('admin.system.selfUpdateStalled') }}
      </div>
      <template v-else-if="upd.update_available && self">
        <button
          v-if="self.available"
          type="button"
          data-testid="self-update-open"
          class="px-4 py-1.5 rounded-md bg-mnema-accent hover:bg-mnema-accent-hover text-mnema-accent-ink text-sm font-semibold transition"
          @click="showConfirm = true"
        >{{ $t('admin.system.selfUpdateButton') }}</button>
        <p v-else-if="self.configured && self.next_allowed_at" class="text-xs text-mnema-muted" data-testid="self-update-cooldown">
          {{ $t('admin.system.selfUpdateCooldown', { when: formatDate(self.next_allowed_at) }) }}
        </p>
        <p v-else-if="self.configured && self.reach === 'no'" class="text-xs text-mnema-muted" data-testid="self-update-unreachable">
          {{ $t(`admin.system.reach.${self.reach_reason}`, { image: self.image, version: upd.latest_version }) }}
        </p>
        <p v-else-if="!self.configured" class="text-xs text-mnema-tertiary" data-testid="self-update-not-configured">
          {{ $t('admin.system.selfUpdateNotConfigured') }}
        </p>
      </template>

      <template v-if="upd.update_available">
        <a
          v-if="upd.release_url"
          :href="upd.release_url"
          target="_blank"
          rel="noopener noreferrer"
          class="text-sm text-mnema-accent hover:underline"
          data-testid="update-release-link"
        >{{ $t('admin.system.releaseNotesLink', { version: upd.latest_version }) }}</a>
        <!-- Release notes are untrusted text: shown as plain text, never as HTML. -->
        <pre
          v-if="upd.release_notes"
          data-testid="update-notes"
          class="max-h-60 overflow-y-auto whitespace-pre-wrap break-words rounded-md bg-mnema-canvas p-3 font-mono text-xs text-mnema-muted"
        >{{ upd.release_notes }}</pre>
        <div class="space-y-1">
          <p class="text-xs text-mnema-muted">{{ $t('admin.system.manualUpdate') }}</p>
          <div class="flex items-center gap-2">
            <code class="min-w-0 flex-1 truncate rounded bg-mnema-canvas px-2 py-1 font-mono text-xs text-mnema-text" data-testid="update-command">{{ UPDATE_COMMAND }}</code>
            <button
              type="button"
              class="flex-shrink-0 rounded-md px-2 py-1 text-xs text-mnema-tertiary hover:bg-mnema-elevated hover:text-mnema-text"
              @click="copyCommand"
            >{{ $t('admin.system.copy') }}</button>
          </div>
        </div>
      </template>
    </div>

    <SelfUpdateDialog
      v-if="showConfirm && upd"
      :current-version="upd.current_version"
      :target-version="upd.latest_version"
      :release-url="upd.release_url"
      @close="showConfirm = false"
      @started="onUpdateStarted"
    />

    <!-- Health -->
    <div v-if="health" class="grid grid-cols-1 md:grid-cols-2 gap-3" data-testid="system-health">
      <div class="bg-mnema-surface p-4 rounded-lg border border-mnema-hairline space-y-2">
        <div class="flex items-center gap-2 text-sm font-semibold text-mnema-text">
          <CheckCircle2 v-if="health.database.reachable" class="w-4 h-4 text-mnema-accent" />
          <XCircle v-else class="w-4 h-4 text-mnema-danger" />
          {{ $t('admin.system.database') }}
        </div>
        <p class="text-sm text-mnema-muted">{{ health.database.reachable ? $t('admin.system.reachable') : $t('admin.system.unreachable') }}</p>
        <p class="text-xs text-mnema-tertiary font-mono break-all">
          {{ $t('admin.system.migration', { name: health.database.latest_migration || '–', count: health.database.applied_migrations }) }}
        </p>
        <p v-if="health.database.pending_migrations" class="text-xs text-mnema-warning">
          {{ $t('admin.system.pendingMigrations', { count: health.database.pending_migrations }) }}
        </p>
      </div>

      <div class="bg-mnema-surface p-4 rounded-lg border border-mnema-hairline space-y-2">
        <div class="flex items-center gap-2 text-sm font-semibold text-mnema-text">
          <CheckCircle2 v-if="health.storage.configured && health.storage.reachable" class="w-4 h-4 text-mnema-accent" />
          <XCircle v-else class="w-4 h-4 text-mnema-danger" />
          {{ $t('admin.system.storage') }}
        </div>
        <p class="text-sm text-mnema-muted">
          {{ !health.storage.configured ? $t('admin.system.notConfigured') : health.storage.reachable ? $t('admin.system.reachable') : $t('admin.system.unreachable') }}
        </p>
        <p class="text-xs text-mnema-tertiary" data-testid="system-storage-size">
          {{ $t('admin.system.storageSize', { total: formatBytes(health.storage.total_bytes), files: health.storage.files, attachments: formatBytes(health.storage.attachment_bytes), avatars: formatBytes(health.storage.avatar_bytes) }) }}
        </p>
      </div>

      <div class="bg-mnema-surface p-4 rounded-lg border border-mnema-hairline space-y-2">
        <div class="flex items-center gap-2 text-sm font-semibold text-mnema-text">
          <CheckCircle2 v-if="health.voice.enabled" class="w-4 h-4 text-mnema-accent" />
          <XCircle v-else class="w-4 h-4 text-mnema-danger" />
          {{ $t('admin.system.voice') }}
        </div>
        <dl class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <dt class="text-mnema-tertiary">{{ $t('admin.system.calls') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ health.voice.rooms }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.participants') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ health.voice.participants }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.screenShares') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ health.voice.screen_shares }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.cameras') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ health.voice.cameras }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.connections') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ health.voice.websocket_connections }} ({{ $t('admin.system.onlineUsers', { count: health.voice.online_users }) }})</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.turn') }}</dt>
          <dd class="text-mnema-text">{{ health.voice.turn_configured ? $t('admin.system.yes') : $t('admin.system.no') }}</dd>
        </dl>
      </div>

      <div class="bg-mnema-surface p-4 rounded-lg border border-mnema-hairline space-y-2">
        <div class="text-sm font-semibold text-mnema-text">{{ $t('admin.system.runtime') }}</div>
        <dl class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <dt class="text-mnema-tertiary">{{ $t('admin.system.uptime') }}</dt>
          <dd class="text-mnema-text" data-testid="system-uptime">{{ formatUptime(health.runtime.uptime_seconds) }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.memory') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ formatBytes(health.runtime.mem_alloc_bytes) }} / {{ formatBytes(health.runtime.mem_sys_bytes) }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.goroutines') }}</dt>
          <dd class="text-mnema-text tabular-nums">{{ health.runtime.goroutines }}</dd>
          <dt class="text-mnema-tertiary">{{ $t('admin.system.goVersion') }}</dt>
          <dd class="text-mnema-text font-mono">{{ health.runtime.go_version }}</dd>
        </dl>
      </div>
    </div>
  </section>
</template>
