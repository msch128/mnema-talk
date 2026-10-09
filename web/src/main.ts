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
import { gamingSurface } from './lib/desktopGaming'
import { isDesktopRuntime } from './lib/desktopRuntime'
import './style.css'
import { i18nPlugin, setLocale, browserLocale } from './i18n'
import { tooltip } from './directives/tooltip'

// Before login the browser decides; the account's language takes over after login.
setLocale(browserLocale())

const root = gamingSurface()
  ? (await import('./components/DesktopGamingOverlay.vue')).default
  : isDesktopRuntime()
  ? (await import('./NativeBootstrap.vue')).default
  : App

createApp(root)
  .use(createPinia())
  .use(i18nPlugin)
  .directive('tooltip', tooltip)
  .mount('#app')
