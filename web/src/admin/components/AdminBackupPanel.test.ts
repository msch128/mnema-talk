import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AdminBackupPanel from './AdminBackupPanel.vue'
import { setLocale } from '../../i18n'
import deMessages from '../../i18n/de.json'
import type { ServerBackupStatus } from '../../types/rest'

const HOUR = 3600

function reported(over: Partial<ServerBackupStatus> = {}): ServerBackupStatus {
  return {
    state: 'ok', warn: false, stale: false, stale_after_hours: 36,
    last_backup_at: '2026-11-06T02:30:00Z', last_backup_age_seconds: 7 * HOUR, size_bytes: 52_428_800,
    restore_test: 'passed', restore_test_age_seconds: 7 * HOUR,
    last_backup_age: '7h0m0s', last_restore_test_age: '7h0m0s', hint: 'doc/operations.md#backups-and-restore',
    ...over,
  }
}

function unreported(state: ServerBackupStatus['state']): ServerBackupStatus {
  return { state, warn: false, stale: false, stale_after_hours: 36, restore_test: 'none', last_backup_age: state, last_restore_test_age: state, hint: 'doc/operations.md#backups-and-restore' }
}

const show = (backup: ServerBackupStatus) => mount(AdminBackupPanel, { props: { backup } })

beforeEach(() => {
  setLocale('en')
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-06T09:30:00Z'))
})
afterEach(() => { vi.useRealTimers() })

describe('AdminBackupPanel', () => {
  it('shows the age and size of the last backup and the passed restore test', () => {
    const w = show(reported())
    expect(w.get('[data-testid="backup-last-age"]').text()).toBe('7 hr. ago · 50 MB')
    expect(w.get('[data-testid="backup-last-restore"]').text()).toBe('Passed, 7 hr. ago')
    expect(w.find('[data-testid="backup-stale"]').exists()).toBe(false)
    expect(w.get('[data-testid="status-backup"]').attributes('data-warn')).toBe('false')
    expect(w.text()).not.toContain('unknown')
  })

  it('rounds the ages, so a test a minute after the backup reads the same, and says just now as ago', () => {
    let w = show(reported({ restore_test_age_seconds: 7 * HOUR - 60 }))
    expect(w.get('[data-testid="backup-last-restore"]').text()).toBe('Passed, 7 hr. ago')
    w.unmount()
    w = show(reported({ last_backup_age_seconds: 20, restore_test_age_seconds: 5 }))
    expect(w.get('[data-testid="backup-last-age"]').text()).toBe('0 min. ago · 50 MB')
    expect(w.get('[data-testid="backup-last-restore"]').text()).toBe('Passed, 0 min. ago')
  })

  it('warns when the backup is older than the threshold', () => {
    const w = show(reported({ stale: true, warn: true, last_backup_age_seconds: 50 * HOUR, restore_test_age_seconds: 50 * HOUR }))
    expect(w.get('[data-testid="backup-last-age"]').text()).toContain('2 days ago')
    expect(w.get('[data-testid="backup-stale"]').text()).toBe('Older than 36 h: check the backup job on the host.')
    expect(w.get('[data-testid="status-backup"]').attributes('data-warn')).toBe('true')
  })

  it('warns when the restore test failed, and says when it was skipped', () => {
    let w = show(reported({ warn: true, restore_test: 'failed' }))
    const restore = w.get('[data-testid="backup-last-restore"]')
    expect(restore.attributes('data-result')).toBe('failed')
    expect(restore.text()).toBe('Failed, 7 hr. ago')
    expect(w.get('[data-testid="backup-restore-failed"]').text()).toContain('may not restore')
    expect(restore.classes()).toContain('text-mnema-warning')
    w.unmount()
    const skipped = reported({ restore_test: 'skipped' })
    delete skipped.restore_test_age_seconds
    w = show(skipped)
    expect(w.find('[data-testid="backup-restore-failed"]').exists()).toBe(false)
    expect(w.get('[data-testid="backup-last-restore"]').text()).toBe('Not run (RESTORE_TEST=0)')
  })

  it('explains how to turn the report on instead of a placeholder', () => {
    const cases: Array<[ServerBackupStatus['state'], string]> = [
      ['not_configured', 'set BACKUP_STATUS_FILE'],
      ['missing', 'Run scripts/backup.sh on the host'],
      ['invalid', 'cannot be read'],
    ]
    for (const [state, text] of cases) {
      const w = show(unreported(state))
      const message = w.get('[data-testid="backup-not-reported"]')
      expect(message.attributes('data-state')).toBe(state)
      expect(message.text()).toContain(text)
      expect(w.find('[data-testid="backup-last-age"]').exists()).toBe(false)
      expect(w.text()).not.toMatch(/unknown|not_configured|missing\b/)
      w.unmount()
    }
  })

  it('keeps the space before the guide link in English and German', () => {
    let w = show(reported())
    const hint = w.get('[data-testid="backup-hint"]')
    expect(hint.text()).toBe('Backups run on the host; see the operations guide.')
    expect(hint.get('a').attributes('href')).toMatch(/\/blob\/main\/doc\/operations\.md#backups-and-restore$/)
    w.unmount()
    setLocale('de')
    w = show(reported())
    const de = deMessages.admin.operatorStatus
    expect(w.get('[data-testid="backup-hint"]').text()).toBe(de.backupDocs.replace('{link}', de.backupDocsLink))
    expect(w.get('[data-testid="backup-hint"]').text()).toMatch(/ \S+\.$/)
    expect(w.get('[data-testid="backup-last-restore"]').text()).toBe(de.restoreTest.passed.replace('{age}', 'vor 7 Std.'))
    expect(w.text()).toContain(de.lastBackupAge)
  })
})
