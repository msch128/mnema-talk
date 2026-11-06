<script setup lang="ts">
// Backups card of Admin > Status: the last backup and its restore test, as scripts/backup.sh
// reported them (GET /api/admin/status, backup). Backups run on the host; without a report it
// says how to turn one on instead of showing a placeholder.
import { computed } from 'vue'
import { ShieldCheck, TriangleAlert } from '@lucide/vue'
import type { ServerBackupStatus } from '../types/rest'
import { SOURCE_URL } from '../lib/thirdParty'
import { formatBytes } from '../lib/byteUnits'
import { t, locale } from '../i18n'

const props = defineProps<{ backup: ServerBackupStatus }>()

const p = 'admin.operatorStatus.'
// Rounded, not truncated: a restore test a minute after a 7-hour-old backup reads "7 hr. ago" too.
const STEPS: Array<[Intl.RelativeTimeFormatUnit, number]> = [['day', 86400], ['hour', 3600], ['minute', 60]]
function ago(seconds: number): string {
  const words = new Intl.RelativeTimeFormat(locale.value, { numeric: 'always', style: 'short' })
  for (const [unit, size] of STEPS) {
    if (seconds >= size) return words.format(-Math.round(seconds / size), unit)
  }
  return words.format(-0, 'minute') // "0 min. ago", not "in 0 min."
}

// The sentence around the guide link, split where the link goes.
const docs = computed(() => {
  const marker = '\u0001'
  const [before = '', after = ''] = t(`${p}backupDocs`, { link: marker }).split(marker)
  return { before, after }
})

// A server before the backup status sends only the two ages as text (no state): show those as they are.
const legacy = computed(() => props.backup.state === undefined)

const view = computed(() => {
  const b = props.backup
  if (b.state !== 'ok' || b.last_backup_age_seconds === undefined) return null
  const restoreAge = b.restore_test_age_seconds === undefined ? '' : ago(b.restore_test_age_seconds)
  return {
    backup: ago(b.last_backup_age_seconds),
    size: b.size_bytes === undefined ? '' : formatBytes(b.size_bytes, locale.value),
    restore: t(`${p}restoreTest.${b.restore_test}`, { age: restoreAge })
  }
})
</script>

<template>
  <div class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-backup" :data-warn="backup.warn ?? false">
    <div class="flex items-center gap-2">
      <ShieldCheck class="w-4 h-4 text-mnema-accent shrink-0" aria-hidden="true" />
      <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.backup') }}</span>
    </div>
    <dl v-if="legacy" class="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm" data-testid="backup-legacy">
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.lastBackupAge') }}</dt>
        <dd class="mt-0.5 text-mnema-text">{{ backup.last_backup_age }}</dd>
      </div>
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.lastRestoreTestAge') }}</dt>
        <dd class="mt-0.5 text-mnema-text">{{ backup.last_restore_test_age }}</dd>
      </div>
    </dl>
    <dl v-else-if="view" class="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.lastBackupAge') }}</dt>
        <dd class="mt-0.5 flex items-center gap-1.5 text-mnema-text" data-testid="backup-last-age">
          <TriangleAlert v-if="backup.stale" class="w-4 h-4 text-mnema-warning shrink-0" aria-hidden="true" />
          <span>{{ view.backup }}<span v-if="view.size" class="text-xs text-mnema-muted">{{ ` · ${view.size}` }}</span></span>
        </dd>
        <p v-if="backup.stale" class="mt-1 text-xs text-mnema-muted" data-testid="backup-stale">{{ $t('admin.operatorStatus.backupStale', { hours: backup.stale_after_hours }) }}</p>
      </div>
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.lastRestoreTestAge') }}</dt>
        <dd class="mt-0.5 flex items-center gap-1.5" :class="backup.restore_test === 'failed' ? 'text-mnema-warning' : 'text-mnema-text'" data-testid="backup-last-restore" :data-result="backup.restore_test">
          <TriangleAlert v-if="backup.restore_test === 'failed'" class="w-4 h-4 shrink-0" aria-hidden="true" />
          <span>{{ view.restore }}</span>
        </dd>
        <p v-if="backup.restore_test === 'failed'" class="mt-1 text-xs text-mnema-muted" data-testid="backup-restore-failed">{{ $t('admin.operatorStatus.restoreFailed') }}</p>
      </div>
    </dl>
    <p v-else-if="backup.state" class="text-sm text-mnema-text" data-testid="backup-not-reported" :data-state="backup.state">{{ $t(`admin.operatorStatus.backupState.${backup.state}`) }}</p>
    <p class="text-xs text-mnema-muted" data-testid="backup-hint">{{ docs.before }}<a :href="`${SOURCE_URL}/blob/main/${backup.hint}`" target="_blank" rel="noopener noreferrer" class="text-mnema-accent hover:underline">{{ $t('admin.operatorStatus.backupDocsLink') }}</a>{{ docs.after }}</p>
  </div>
</template>
