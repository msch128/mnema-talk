import { ref } from 'vue'

// How the side columns behave on a window too narrow for all of them. The
// shell (app/composables/useSideColumns) decides; the headers read it, so the
// member button tells the truth and the thread offers a way back.

/** The thread opens over the chat (with a back button) because a column would squeeze the chat below its minimum. */
export const threadOverlay = ref(false)
/** The member list gave way to an open thread for lack of room. */
export const membersSteppedAside = ref(false)
/** The member asked for the list anyway: it opens over the chat until closed. */
export const membersOverlay = ref(false)

/** Whether the member list is on screen, for the member button's pressed state. */
export function memberListShown(showMemberList: boolean): boolean {
  return showMemberList && (!membersSteppedAside.value || membersOverlay.value)
}

/** The member button: shows the list (over the chat when it stepped aside) or hides it. */
export function toggleMemberList(chat: { showMemberList: boolean }): void {
  if (memberListShown(chat.showMemberList)) {
    chat.showMemberList = false
    membersOverlay.value = false
    return
  }
  chat.showMemberList = true
  if (membersSteppedAside.value) membersOverlay.value = true
}
