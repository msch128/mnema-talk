// For a one-off form that creates something (a channel, a post, a poll): closing it with
// typed input asks "Discard changes?" first, and so does closing the page. Pass the result
// as `BaseDialog :guard` and call the dialog's `close` slot prop from Cancel.
import { computed } from 'vue'
import { confirmDiscard, useLeaveGuard } from './draftGuard'

export function useDiscardGuard(hasInput: () => boolean) {
  const typed = computed(hasInput)
  useLeaveGuard(typed)
  return async () => !typed.value || confirmDiscard()
}
