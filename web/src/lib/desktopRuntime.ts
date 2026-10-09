import { isTauri } from '@tauri-apps/api/core'

/** Only a local Tauri host selects native IPC. Browser requests stay same-origin. */
export function isDesktopRuntime(): boolean {
  if (typeof window === 'undefined' || !isTauri()) return false
  const { protocol, hostname, port } = window.location
  return !port && hostname === 'localhost' && protocol === 'tauri:'
    || !port && hostname === 'tauri.localhost' && protocol === 'http:'
}

/** The bundled selector has IPC; a selected HTTPS instance uses browser APIs. */
export function isWebDesktopSelector(): boolean {
  return isDesktopRuntime() && Reflect.get(window, '__MNEMA_WEB_DESKTOP__') === true
}
