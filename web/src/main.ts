import { createApp } from 'vue'
import { createPinia } from 'pinia'
// Fonts are bundled locally: loading them from Google would send every
// visitor's IP address to a third party (and the CSP blocks it anyway).
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import App from './App.vue'
import { gamingSurface } from './talk/lib/desktopGaming'
import { platform } from './shared/lib/platform'
import { captureInstallPrompt, registerServiceWorker } from './shared/lib/pwa'
import { installRangeFill } from './shared/lib/rangeFill'
import { unlockAudioOnFirstGesture } from './talk/lib/soundEffects'
import './style.css'
import { i18nPlugin, setLocale, browserLocale } from './i18n'
import { maxChars } from './directives/maxChars'
import { tooltip } from './directives/tooltip'
import { fullText } from './directives/fullText'

// Before login the browser decides; the account's language takes over after login.
setLocale(browserLocale())

const root = gamingSurface()
  ? (await import('./talk/components/DesktopGamingOverlay.vue')).default
  : platform.isWebDesktopSelector()
    ? (await import('./app/components/DesktopSelector.vue')).default
    : App

createApp(root)
  .use(createPinia())
  .use(i18nPlugin)
  .directive('tooltip', tooltip)
  .directive('fullText', fullText)
  .directive('maxChars', maxChars)
  .mount('#app')

installRangeFill()
unlockAudioOnFirstGesture()
captureInstallPrompt()
registerServiceWorker()
