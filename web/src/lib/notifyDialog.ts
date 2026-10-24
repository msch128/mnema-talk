import { ref } from 'vue'

/** Whether the "Notification settings" dialog (server-wide default) is open; the menus open it, App.vue shows it. */
export const notifyDialogOpen = ref(false)
