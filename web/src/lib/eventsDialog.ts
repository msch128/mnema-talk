import { ref } from 'vue'

/**
 * Whether the events list is asked for (and which event to show in it); App.vue
 * shows it, a reminder or notification asks. `seq` changes with every ask, so
 * asking again for the same event shows it again.
 */
export const eventsRequest = ref<{ eventId: string | null; seq: number } | null>(null)

let asks = 0

/** Opens the events list, on one event when it is named. */
export function openEvents(eventId: string | null = null) {
  eventsRequest.value = { eventId, seq: ++asks }
}

export function closeEvents() {
  eventsRequest.value = null
}
