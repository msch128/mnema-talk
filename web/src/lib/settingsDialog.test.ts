import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAccountStore } from '../stores/account'
import { focusSettingsField, isSettingsTab, openSettings, rememberSettingsTab, SETTINGS_TABS, settingsField, settingsTab } from './settingsDialog'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('settings dialog sections', () => {
  it('lists this device first, so the microphone is the first thing to find', () => {
    expect(SETTINGS_TABS[0]).toBe('audio')
    expect(SETTINGS_TABS.indexOf('app')).toBeLessThan(SETTINGS_TABS.indexOf('signin'))
    expect(isSettingsTab('roles')).toBe(true)
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

describe('openSettings with a field', () => {
  beforeEach(() => { settingsField.value = null })


  it('opens the dialog on a section and a field', () => {
    openSettings('audio', { field: 'input-device' })
    expect(useAccountStore().dialogOpen).toBe(true)
    expect(settingsTab.value).toBe('audio')
    expect(settingsField.value).toBe('input-device')
  })

  it('forgets an earlier field when opened without one', () => {
    settingsField.value = 'noise'
    openSettings()
    expect(settingsField.value).toBeNull()
  })

  it('moves an open dialog instead of reopening it, so what it is loading is kept', () => {
    const account = useAccountStore()
    const open = vi.spyOn(account, 'open')
    openSettings('audio')
    openSettings('app', { field: 'push' })
    expect(open).toHaveBeenCalledTimes(1)
    expect(settingsTab.value).toBe('app')
  })

  it('knows the section names', () => {
    expect(isSettingsTab('audio')).toBe(true)
    expect(isSettingsTab('nope')).toBe(false)
    expect(isSettingsTab(undefined)).toBe(false)
  })
})

describe('focusSettingsField', () => {
  it('scrolls a control into view and focuses it', () => {
    document.body.innerHTML = '<div id="r"><select data-settings-field="input-device"></select></div>'
    const select = document.querySelector('select') as HTMLSelectElement
    select.scrollIntoView = vi.fn()
    expect(focusSettingsField(document.getElementById('r') as HTMLElement, 'input-device')).toBe(true)
    expect(select.scrollIntoView).toHaveBeenCalled()
    expect(document.activeElement).toBe(select)
  })

  it('focuses the chosen option of a group', () => {
    document.body.innerHTML = '<div id="r"><div data-settings-field="noise" role="radiogroup"><button role="radio" aria-checked="false" tabindex="-1">a</button><button role="radio" aria-checked="true" tabindex="0">b</button></div></div>'
    expect(focusSettingsField(document.getElementById('r') as HTMLElement, 'noise')).toBe(true)
    expect(document.activeElement?.textContent).toBe('b')
  })

  it('reports a field that is not there', () => {
    document.body.innerHTML = '<div id="r"></div>'
    expect(focusSettingsField(document.getElementById('r') as HTMLElement, 'noise')).toBe(false)
  })
})
