<script setup lang="ts">
// Operator status tab: server version, updates, database, storage, backups, activity and warnings (GET /api/admin/status).
import { ref, computed, onMounted } from 'vue'
import { RefreshCw, CheckCircle2, XCircle, TriangleAlert, CircleHelp, Database, HardDrive, ShieldCheck, Activity, Server } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { OperatorStatus } from '../types/domain'
import { decodeOperatorStatus } from '../types/domain'
import { useToastStore } from '../stores/toast'
import { SOURCE_URL } from '../lib/thirdParty'
import { describeWarning } from '../lib/operatorWarnings'
import { describeCheck } from '../lib/setupChecks'
import { t, locale } from '../i18n'
import { useChatStore } from '../stores/chat'
import { reportWebSocket } from '../lib/wsReport'
import ConnectionTest from './ConnectionTest.vue'
import AdminCapacityPanel from './AdminCapacityPanel.vue'

const toasts = useToastStore()
const chat = useChatStore()
const status = ref<OperatorStatus | null>(null)
const loading = ref(false)

async function refresh() {
  loading.value = true
  try {
    status.value = await api('/api/admin/status', { decode: decodeOperatorStatus })
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    loading.value = false
  }
}

function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined) return '–'
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)))
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return '–'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '–'
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(d)
}

const warnings = computed(() => (status.value?.warnings || []).map(w => ({ ...w, ...describeWarning(w.code, w.value) })))
const setup = computed(() => (status.value?.setup || []).map(c => ({ ...c, ...describeCheck(c) })))
const setupAttention = computed(() => setup.value.filter(c => c.level === 'warn' || c.level === 'fail').length)
// Red items first, then amber, then neutral (not verifiable from here), then green; the server order is kept within a level.
const LEVEL_RANK = { fail: 0, warn: 1, info: 2, ok: 3 } as const
const setupSorted = computed(() => [...setup.value].sort((a, b) => LEVEL_RANK[a.level] - LEVEL_RANK[b.level]))
const checkLabels = new Set(['public_url', 'voice_address', 'voice_udp', 'turn', 'mail', 'storage', 'database', 'clock', 'reverse_proxy', 'websocket'])
const checkLabel = (id: string) => (checkLabels.has(id) ? t(`admin.operatorStatus.check.${id}`) : id)
const db = computed(() => status.value?.database || null)
const storage = computed(() => status.value?.storage || null)
const activity = computed(() => status.value?.activity || null)
const backup = computed(() => status.value?.backup || null)
const upd = computed(() => status.value?.update || null)
const autoDeploy = computed(() => status.value?.auto_deploy || null)

// Show the list at once, then again with the WebSocket item the report just fed.
onMounted(async () => {
  void refresh()
  await reportWebSocket(() => chat.isConnected)
  await refresh()
})
</script>

<template>
  <section class="space-y-5" data-testid="admin-status">
    <div class="flex items-center justify-between gap-3">
      <h3 class="flex items-center gap-2 text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider"><Activity class="w-4 h-4 text-mnema-accent" /><span>{{ $t('admin.operatorStatus.heading') }}</span></h3>
      <button
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :disabled="loading"
        :title="$t('admin.refresh')"
        :aria-label="$t('admin.refresh')"
        @click="refresh"
      >
        <RefreshCw class="w-4 h-4" :class="loading && 'animate-spin'" />
      </button>
    </div>

    <!-- Setup checks -->
    <div v-if="setup.length" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" role="region" :aria-label="$t('admin.operatorStatus.setup')" data-testid="status-setup">
      <div class="flex items-center justify-between gap-2">
        <div class="flex items-center gap-2">
          <ShieldCheck class="w-4 h-4 text-mnema-accent shrink-0" aria-hidden="true" />
          <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.setup') }}</span>
        </div>
        <p class="text-xs text-mnema-muted" data-testid="setup-summary">
          {{ setupAttention ? $t('admin.operatorStatus.setupSummaryAttention', { count: setupAttention, total: setup.length }) : $t('admin.operatorStatus.setupSummaryOk') }}
        </p>
      </div>
      <ul class="divide-y divide-mnema-hairline">
        <li v-for="c in setupSorted" :key="c.id" class="py-2.5 text-sm" :data-testid="`setup-check-${c.id}`" :data-level="c.level">
          <div class="flex items-start gap-2.5">
            <XCircle v-if="c.level === 'fail'" class="w-4 h-4 mt-0.5 text-mnema-danger shrink-0" aria-hidden="true" />
            <TriangleAlert v-else-if="c.level === 'warn'" class="w-4 h-4 mt-0.5 text-mnema-warning shrink-0" aria-hidden="true" />
            <CircleHelp v-else-if="c.level === 'info'" class="w-4 h-4 mt-0.5 text-mnema-tertiary shrink-0" aria-hidden="true" />
            <CheckCircle2 v-else class="w-4 h-4 mt-0.5 text-mnema-success shrink-0" aria-hidden="true" />
            <div class="min-w-0 space-y-1">
              <p class="font-medium text-mnema-text">
                {{ checkLabel(c.id) }}
                <span class="sr-only"> – {{ $t(`admin.operatorStatus.level.${c.level}`) }}</span>
              </p>
              <p class="text-xs text-mnema-muted break-words">{{ c.detail }}</p>
              <p v-if="c.fix" class="text-xs text-mnema-text break-words" :data-testid="`setup-fix-${c.id}`">{{ c.fix }}</p>
            </div>
          </div>
        </li>
      </ul>
    </div>

    <div class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline" data-testid="status-connection-test">
      <ConnectionTest />
    </div>

    <!-- Operator warnings -->
    <div v-if="warnings.length" class="rounded-lg border border-mnema-warning/35 bg-mnema-warning/10 p-4 space-y-3" role="region" :aria-label="$t('admin.operatorStatus.warnings')" data-testid="status-warnings">
      <div class="flex items-center gap-2 text-sm font-semibold text-mnema-text">
        <TriangleAlert class="w-4 h-4 text-mnema-warning shrink-0" aria-hidden="true" />
        {{ $t('admin.operatorStatus.warnings') }}
      </div>
      <ul class="space-y-3">
        <li v-for="w in warnings" :key="w.code" class="text-sm" :data-testid="`status-warning-${w.code}`">
          <p class="font-medium text-mnema-text">{{ w.title }}</p>
          <p class="text-mnema-muted">{{ w.body }}</p>
          <p class="font-mono text-xs text-mnema-tertiary">{{ w.code }}</p>
          <p class="text-xs text-mnema-muted">{{ $t('admin.operatorStatus.count', { count: w.count }) }} · {{ $t('admin.operatorStatus.since', { since: formatDate(w.since) }) }}</p>
          <a :href="w.doc" target="_blank" rel="noopener noreferrer" class="text-xs text-mnema-accent hover:underline">{{ $t('admin.system.warn.docs') }}</a>
        </li>
      </ul>
    </div>

    <AdminCapacityPanel />

    <!-- Live Activity -->
    <div v-if="activity" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-activity">
      <div class="flex items-center gap-2">
        <Activity class="w-4 h-4 text-mnema-accent shrink-0" />
        <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.activity') }}</span>
      </div>
      <dl class="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.activeSessions') }}</dt>
          <dd class="text-lg font-bold font-mono text-mnema-text" data-testid="activity-active-sessions">{{ activity.active_sessions }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.connectedWs') }}</dt>
          <dd class="text-lg font-bold font-mono text-mnema-text" data-testid="activity-connected-ws">{{ activity.connected_websocket_clients }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.activeTalks') }}</dt>
          <dd class="text-lg font-bold font-mono text-mnema-text" data-testid="activity-active-talks">{{ activity.active_talks }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.voiceParticipants') }}</dt>
          <dd class="text-lg font-bold font-mono text-mnema-text" data-testid="activity-voice-participants">{{ activity.voice_participants }}</dd>
        </div>
      </dl>
    </div>

    <!-- Version & Updates -->
    <div v-if="status" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-version">
      <div class="flex items-center gap-2">
        <Server class="w-4 h-4 text-mnema-accent shrink-0" />
        <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.version') }}</span>
      </div>
      <dl class="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.system.version') }}</dt>
          <dd class="text-base font-bold font-mono text-mnema-text" data-testid="version-current">{{ status.version.current }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.revision') }}</dt>
          <dd class="font-mono text-mnema-text">{{ status.version.revision || '–' }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.goVersion') }}</dt>
          <dd class="font-mono text-mnema-text">{{ status.version.go_version }}</dd>
        </div>
      </dl>
      <div v-if="upd" class="pt-2 border-t border-mnema-hairline text-sm flex flex-wrap items-center justify-between gap-2">
        <div>
          <p v-if="upd.update_available" class="font-semibold text-mnema-accent" data-testid="status-update-available">
            {{ $t('admin.operatorStatus.updateAvailable', { latest: upd.latest_version }) }}
          </p>
          <p v-else-if="upd.check_enabled" class="text-mnema-muted" data-testid="status-up-to-date">
            {{ $t('admin.operatorStatus.upToDate') }}
          </p>
          <p v-else class="text-mnema-muted" data-testid="status-check-disabled">
            {{ $t('admin.operatorStatus.checkDisabled') }}
          </p>
        </div>
        <div v-if="autoDeploy" class="text-xs text-mnema-tertiary" data-testid="status-auto-deploy">
          <span v-if="autoDeploy.image">{{ autoDeploy.image }} ({{ autoDeploy.reach }})</span>
        </div>
      </div>
    </div>

    <!-- Database -->
    <div v-if="db" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-database">
      <div class="flex items-center gap-2">
        <Database class="w-4 h-4 text-mnema-accent shrink-0" />
        <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.database') }}</span>
      </div>
      <dl class="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.reachable') }}</dt>
          <dd class="flex items-center gap-1 font-medium mt-0.5" data-testid="db-reachable">
            <CheckCircle2 v-if="db.reachable" class="w-4 h-4 text-mnema-success shrink-0" />
            <XCircle v-else class="w-4 h-4 text-mnema-danger shrink-0" />
            <span :class="db.reachable ? 'text-mnema-text' : 'text-mnema-danger'">{{ db.reachable ? $t('admin.operatorStatus.reachable') : $t('admin.operatorStatus.unreachable') }}</span>
          </dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.dbSize') }}</dt>
          <dd class="font-mono text-mnema-text" data-testid="db-size">{{ formatBytes(db.size_bytes) }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.latestMigration') }}</dt>
          <dd class="font-mono text-mnema-text truncate text-xs" :title="db.latest_migration" data-testid="db-latest-migration">{{ db.latest_migration || '–' }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.appliedMigrations') }}</dt>
          <dd class="font-mono text-mnema-text" data-testid="db-applied-migrations">{{ db.applied_migrations }} <span v-if="db.pending_migrations" class="text-mnema-warning font-normal text-xs">({{ $t('admin.system.pendingMigrations', { count: db.pending_migrations }) }})</span></dd>
        </div>
      </dl>
    </div>

    <!-- Storage & Disk -->
    <div v-if="storage" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-storage">
      <div class="flex items-center gap-2">
        <HardDrive class="w-4 h-4 text-mnema-accent shrink-0" />
        <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.storage') }}</span>
      </div>
      <dl class="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.configured') }}</dt>
          <dd class="text-mnema-text mt-0.5" data-testid="storage-configured">{{ storage.configured ? $t('admin.operatorStatus.configured') : $t('admin.operatorStatus.notConfigured') }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.reachable') }}</dt>
          <dd class="flex items-center gap-1 font-medium mt-0.5" data-testid="storage-reachable">
            <CheckCircle2 v-if="storage.reachable" class="w-4 h-4 text-mnema-success shrink-0" />
            <XCircle v-else class="w-4 h-4 text-mnema-danger shrink-0" />
            <span :class="storage.reachable ? 'text-mnema-text' : 'text-mnema-danger'">{{ storage.reachable ? $t('admin.operatorStatus.reachable') : $t('admin.operatorStatus.unreachable') }}</span>
          </dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.storageUsed') }}</dt>
          <dd class="font-mono text-mnema-text" data-testid="storage-used-bytes">{{ formatBytes(storage.used_bytes) }}</dd>
        </div>
        <div v-if="status?.disk_free_bytes !== undefined">
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.diskFree') }}</dt>
          <dd class="font-mono text-mnema-text" data-testid="storage-disk-free">{{ formatBytes(status.disk_free_bytes) }}</dd>
        </div>
      </dl>
    </div>

    <!-- Backups & Recovery -->
    <div v-if="backup" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-backup">
      <div class="flex items-center gap-2">
        <ShieldCheck class="w-4 h-4 text-mnema-accent shrink-0" />
        <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.backup') }}</span>
      </div>
      <dl class="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.lastBackupAge') }}</dt>
          <dd class="font-mono text-mnema-text mt-0.5" data-testid="backup-last-age">{{ backup.last_backup_age }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.lastRestoreTestAge') }}</dt>
          <dd class="font-mono text-mnema-text mt-0.5" data-testid="backup-last-restore">{{ backup.last_restore_test_age }}</dd>
        </div>
        <div>
          <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.backupHostManaged') }}</dt>
          <dd class="text-xs text-mnema-muted mt-0.5" data-testid="backup-hint">
            <span>{{ $t('admin.operatorStatus.backupDocsHint') }} </span>
            <a :href="`${SOURCE_URL}/blob/main/${backup.hint}`" target="_blank" rel="noopener noreferrer" class="text-mnema-accent hover:underline">{{ $t('admin.operatorStatus.backupDocsLink') }}</a>
          </dd>
        </div>
      </dl>
    </div>
  </section>
</template>
