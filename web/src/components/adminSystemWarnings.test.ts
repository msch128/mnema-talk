import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import type { ServerBuiltinTURNHealth } from '../types/rest'
import { builtinTurnText } from './adminSystemWarnings'

const relay = (change: Partial<ServerBuiltinTURNHealth>): ServerBuiltinTURNHealth => ({
  mode: 'auto', enabled: true, active: true, port: 3479, udp: true, tcp: true, allocations: 0, host: 'talk.example.test', reachability: 'unknown', ...change,
})

describe('builtinTurnText', () => {
  beforeEach(() => setLocale('en'))
  it('is empty without data', () => {
    expect(builtinTurnText(undefined)).toBe('')
  })
  it('says off, not running, or what is listening', () => {
    expect(builtinTurnText(relay({ active: false, enabled: false }))).toBe('Off')
    expect(builtinTurnText(relay({ active: false }))).toBe('Not running')
    expect(builtinTurnText(relay({ tcp: false, allocations: 3 }))).toContain('Port 3479 (UDP), 3 relayed')
    expect(builtinTurnText(relay({ udp: false }))).toContain('(TCP)')
    expect(builtinTurnText(relay({ reachability: 'unreachable' }))).toContain('check the port forward')
  })
})
