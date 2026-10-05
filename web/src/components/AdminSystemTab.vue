<script setup>
// Admin dashboard: version, health and load of the server (GET /api/admin/system).
import { ref, computed, onMounted } from 'vue'
import { RefreshCw, CheckCircle2, XCircle } from '@lucide/vue'
import { api } from '../lib/api'
import { useToastStore } from '../stores/toast'
import { useAppVersionStore } from '../stores/appVersion'
import { t, locale } from '../i18n'

const toasts = useToastStore()
const versionStore = useAppVersionStore()

const status = ref(null)
const loading = ref(false)

async function refresh() {
  loading.value = true
  try {
    status.value = await api('/api/admin/system')
  } catch (e) {
    toasts.error(e?.message || t('admin.unknownError'))
  } finally {
    loading.value = false
  }
}

defineExpose({ refresh, status })

const health = computed(() => status.value?.health || null)

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)))
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

function formatUptime(seconds) {
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
