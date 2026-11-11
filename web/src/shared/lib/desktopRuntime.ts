import { isTauri } from '@tauri-apps/api/core'
import { trustedApp } from './native/trustedDesktop'

/** The legacy local Tauri origin that hosts the desktop client's instance selector page. */
export function isDesktopRuntime(): boolean {
  if (typeof window === 'undefined' || !isTauri()) return false
  const { protocol, hostname, port } = window.location
  return !port && hostname === 'localhost' && protocol === 'tauri:'
    || !port && hostname === 'tauri.localhost' && protocol === 'http:'
}

/**
 * The instance selector of the desktop client: either the legacy page on the
 * Tauri origin or the verified bundle in its selector role. A selected
 * instance runs the app with browser APIs.
 */
export function isWebDesktopSelector(): boolean {
  return isDesktopRuntime() && Reflect.get(window, '__MNEMA_WEB_DESKTOP__') === true || trustedApp()?.role === 'selector'
}
