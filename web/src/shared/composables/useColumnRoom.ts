import { ref } from 'vue'

// Whether the window is wide enough for the thread and the member list beside
// the chat, each at its narrowest (App.vue: channel list 200, thread 320,
// member list 200, and 400 for the chat itself). Below that, a window just
// above the phone layout squeezed the chat to a sliver; there the member list
// steps aside while a thread is open and comes back when it closes.
const ALL_COLUMNS_QUERY = '(min-width: 1120px)'

const roomForAllColumns = ref(true)
let started = false

function start() {
  if (started || typeof window === 'undefined' || !window.matchMedia) return
  started = true
  const query = window.matchMedia(ALL_COLUMNS_QUERY)
  roomForAllColumns.value = query.matches
  query.addEventListener('change', (event) => { roomForAllColumns.value = event.matches })
}

/** True while the thread, the member list and the chat all fit side by side. */
export function useColumnRoom() {
  start()
  return roomForAllColumns
}
