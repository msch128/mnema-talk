import { isTauri } from '@tauri-apps/api/core'

/** Only a local Tauri host selects native IPC. Browser requests stay same-origin. */
export function isDesktopRuntime(): boolean {
  return typeof window !== 'undefined' && isTauri()
}
