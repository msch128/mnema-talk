import { ref } from 'vue'
import { platform } from './platform'

/**
 * The installable web app: the service worker that keeps the app shell
 * available offline (and, with notifications on, receives pushes), and the
 * state of the browser's install prompt.
 */

/** Whether this page may run the service worker. The desktop client has its own host and custom protocols, where one could only get in the way. */
export function serviceWorkerAllowed(): boolean {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return false
  if (platform.isTrustedDesktop() || platform.isWebDesktopSelector()) return false
  return window.location.protocol === 'https:' || window.location.protocol === 'http:'
}

/** Registers /sw.js once the page has loaded; failures leave the app working as a plain web page. Only built releases carry the worker. */
export function registerServiceWorker(): void {
  if (!import.meta.env.PROD || !serviceWorkerAllowed()) return
  const register = () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/', updateViaCache: 'none' }).catch(error => {
      console.warn('Service worker not registered:', error)
    })
  }
  if (document.readyState === 'complete') register()
  else window.addEventListener('load', register, { once: true })
}

/** The browser's install prompt (Chromium): held back until the person asks for it. */
interface InstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export type InstallState = 'installed' | 'available' | 'ios' | 'unavailable'

let deferred: InstallPromptEvent | null = null
const available = ref(false)
const installed = ref(false)

/** The page runs as the installed app (own window, no browser bar). */
export function isStandalone(): boolean {
  if (typeof window === 'undefined') return false
  const ios = (navigator as Navigator & { standalone?: boolean }).standalone === true
  return ios || window.matchMedia?.('(display-mode: standalone)').matches === true
}

/** iPhone and iPad, including an iPad that presents itself as a Mac. */
export function isIosDevice(): boolean {
  if (typeof navigator === 'undefined') return false
  return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

/** Starts listening for the install prompt; call once at start, before the browser offers it. */
export function captureInstallPrompt(): void {
  window.addEventListener('beforeinstallprompt', event => {
    event.preventDefault()
    deferred = event as InstallPromptEvent
    available.value = true
  })
  window.addEventListener('appinstalled', () => {
    deferred = null
    available.value = false
    installed.value = true
  })
}

/** What the settings can offer for installing this app right now. */
export function installState(): InstallState {
  if (installed.value || isStandalone()) return 'installed'
  if (available.value) return 'available'
  return isIosDevice() ? 'ios' : 'unavailable'
}

/** Shows the browser's install prompt; resolves true when the person accepted. */
export async function promptInstall(): Promise<boolean> {
  const event = deferred
  if (!event) return false
  deferred = null
  available.value = false
  await event.prompt()
  const { outcome } = await event.userChoice
  installed.value = outcome === 'accepted'
  return installed.value
}
