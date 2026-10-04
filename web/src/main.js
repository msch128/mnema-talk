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
import './style.css'

createApp(App).use(createPinia()).mount('#app')
