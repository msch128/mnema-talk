import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAccountStore } from '../stores/account'
import { openSettings, rememberSettingsTab, SETTINGS_TABS, settingsTab } from './settingsDialog'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('settings dialog sections', () => {
  it('lists this device first, so the microphone is the first thing to find', () => {
    expect(SETTINGS_TABS[0]).toBe('audio')
    expect(SETTINGS_TABS.indexOf('app')).toBeLessThan(SETTINGS_TABS.indexOf('signin'))
  })

  it('opens on the section the member used last, and on the first one before that', () => {
    openSettings()
    expect(settingsTab.value).toBe('audio')
    rememberSettingsTab('privacy')
    useAccountStore().close()
    openSettings()
    expect(settingsTab.value).toBe('privacy')
    expect(useAccountStore().dialogOpen).toBe(true)
  })

  it('opens on the asked-for section without making it the one remembered', () => {
    rememberSettingsTab('privacy')
    openSettings('data')
    expect(settingsTab.value).toBe('data')
    openSettings()
    expect(settingsTab.value).toBe('privacy')
  })

  it('ignores a stored section that no longer exists', () => {
    localStorage.setItem('mnema_settings_tab', 'gone')
    openSettings()
    expect(settingsTab.value).toBe('audio')
  })
})
