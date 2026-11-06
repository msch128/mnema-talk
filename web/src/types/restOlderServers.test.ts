import { describe, expect, it } from 'vitest'
import {
  decodeDiscordimportPlan, decodeDiscordimportPlanChannel, decodeServerBackupStatus, decodeServerCapacityEstimate,
} from './rest'

// A server from before these fields existed must still be readable by a newer web app.
describe('responses of older servers decode', () => {
  it('a backup status that has only the two ages and the hint', () => {
    const old = { last_backup_age: 'unknown', last_restore_test_age: 'unknown', hint: 'doc/operations.md#backups-and-restore' }
    expect(decodeServerBackupStatus(old)).toEqual(old)
  })

  it('a capacity estimate without scenarios and upload limit', () => {
    const old = { basis: 'table', more_members: 12, per_member_millicores: 3.5, plan_cores: 2, scale: 1, status: 'ok' }
    expect(decodeServerCapacityEstimate(old)).toEqual(old)
  })

  it('a Discord import channel without note hints', () => {
    const old = {
      source_id: '1', category_source_id: '', category: '', name: 'general', type: 'text', source_type: 'text', topic: '',
      slow_mode_seconds: 0, user_limit: 0, private: false, overwrites: [], notes: ['a note'],
    }
    expect(decodeDiscordimportPlanChannel(old)).toEqual(old)
  })

  it('a Discord import plan without skipped hints', () => {
    const old = {
      administrator_dropped: false, categories: [], channels: [], code: 'abc', counts: { categories: 0, channels: 0, roles: 0 },
      dropped_bits: [], everyone: { applied: false, dropped: [], permission_keys: [] }, hash: 'h', mappable_roles: [],
      mapped_roles: [], roles: [], server_name: 's', skipped: ['a thread'], source_name: 's', template_name: 't',
    }
    expect(decodeDiscordimportPlan(old)).toEqual(old)
  })
})
