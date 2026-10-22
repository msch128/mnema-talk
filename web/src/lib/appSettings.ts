import { ref } from 'vue'

/** Whether the "App" dialog (install on this device) is open; the account menu opens it, App.vue shows it. */
export const appSettingsOpen = ref(false)
