import { describe, it, expect } from 'vitest'
import { isNewServerVersion, normalizeVersion, CLIENT_VERSION } from './appVersion'
import { appVersion } from '../../version.config.js'

describe('appVersion', () => {
  it('embeds a version string', () => {
    expect(typeof CLIENT_VERSION).toBe('string')
    expect(CLIENT_VERSION.length).toBeGreaterThan(0)
  })

  it('offers a reload only for a different, known release', () => {
    expect(isNewServerVersion('0.4.0', '0.3.0')).toBe(true)
    expect(isNewServerVersion('v0.4.0', '0.4.0')).toBe(false)
    expect(isNewServerVersion('0.3.0', '0.3.0')).toBe(false)
    // A rollback is a different build as well.
    expect(isNewServerVersion('0.2.0', '0.3.0')).toBe(true)
  })

  it('never offers a reload for development builds or junk', () => {
    expect(isNewServerVersion('dev', '0.3.0')).toBe(false)
    expect(isNewServerVersion('0.3.0', 'dev')).toBe(false)
    expect(isNewServerVersion('', '0.3.0')).toBe(false)
    expect(isNewServerVersion(undefined, '0.3.0')).toBe(false)
    expect(isNewServerVersion('<img src=x>', '0.3.0')).toBe(false)
    expect(normalizeVersion(42)).toBe('')
  })

  it('takes the build version from MNEMA_VERSION, else version.txt', () => {
    expect(appVersion({ MNEMA_VERSION: '1.2.3' })).toBe('1.2.3')
    expect(appVersion({ MNEMA_VERSION: 'v1.2.3' })).toBe('1.2.3')
    expect(appVersion({}, () => '0.9.0\n')).toBe('0.9.0')
    expect(appVersion({}, () => { throw new Error('missing') })).toBe('dev')
    expect(appVersion({ MNEMA_VERSION: 'bad value' })).toBe('dev')
  })
})
