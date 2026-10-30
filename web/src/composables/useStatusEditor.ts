import { ref } from 'vue'

// The status dialog is opened from the presence menu and from one's own
// profile and hosted once, by the app shell.
const open = ref(false)

export function useStatusEditor() {
  return {
    open,
    show: () => { open.value = true },
    hide: () => { open.value = false }
  }
}
