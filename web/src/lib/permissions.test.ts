import { describe, expect, it } from 'vitest'
import {
  ADMIN_AREA_KEYS, CHANNEL_PERMISSION_KEYS, DEFAULT_EVERYONE, PERMISSION_BITS, PERMISSION_KEYS, ROLE_COLOR_PRESETS,
  hasPermission, parseRoleColor, roleColorCss, setPermission
} from './permissions'

describe('permission bits', () => {
  it('are distinct single bits in display order', () => {
    const seen = new Set<number>()
    for (const key of PERMISSION_KEYS) {
      const bit = PERMISSION_BITS[key]
      expect(Number.isInteger(Math.log2(bit))).toBe(true)
      expect(seen.has(bit)).toBe(false)
      seen.add(bit)
    }
    expect(PERMISSION_KEYS[0]).toBe('administrator')
  })

  it('keep channel and admin groups inside the table', () => {
    for (const key of [...CHANNEL_PERMISSION_KEYS, ...ADMIN_AREA_KEYS]) expect(PERMISSION_KEYS).toContain(key)
    expect(CHANNEL_PERMISSION_KEYS).not.toContain('manage_roles')
    expect(ADMIN_AREA_KEYS).not.toContain('administrator')
  })

  it('default @everyone to what members could always do', () => {
    expect(DEFAULT_EVERYONE).toBe(990)
    for (const key of ['view_channel', 'send_messages', 'attach_files', 'add_reactions', 'mention_everyone', 'connect', 'speak', 'video'] as const) {
      expect(hasPermission(DEFAULT_EVERYONE, key)).toBe(true)
    }
    for (const key of ['administrator', 'manage_messages', 'manage_channels', 'manage_roles', 'create_invites', 'manage_members'] as const) {
      expect(hasPermission(DEFAULT_EVERYONE, key)).toBe(false)
    }
  })

  it('are set and cleared one at a time', () => {
    const withSpeak = setPermission(0, 'speak', true)
    expect(hasPermission(withSpeak, 'speak')).toBe(true)
    expect(hasPermission(withSpeak, 'video')).toBe(false)
    expect(setPermission(withSpeak, 'speak', false)).toBe(0)
    expect(setPermission(withSpeak, 'speak', true)).toBe(withSpeak)
  })
})

describe('role colours', () => {
  it('render as CSS, none for 0', () => {
    expect(roleColorCss(0)).toBe('')
    expect(roleColorCss(0xff0000)).toBe('#ff0000')
    expect(roleColorCss(0x00ff)).toBe('#0000ff')
  })

  it('parse with or without the hash', () => {
    expect(parseRoleColor('#FF0000')).toBe(0xff0000)
    expect(parseRoleColor(' 00ff00 ')).toBe(0x00ff00)
    expect(parseRoleColor('#fff')).toBeNull()
    expect(parseRoleColor('nope')).toBeNull()
    expect(parseRoleColor('')).toBeNull()
  })

  it('offers valid presets', () => {
    for (const color of ROLE_COLOR_PRESETS) expect(parseRoleColor(roleColorCss(color))).toBe(color)
  })
})
