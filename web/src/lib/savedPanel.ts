// A request to open the header's "Saved & reminders" panel from elsewhere (the
// toast after saving, a due reminder). The panel in the channel header takes
// the request and clears it, like lib/laterDialog does for its dialog.
import { shallowRef } from 'vue'

export type PanelTab = 'saved' | 'later'

export interface PanelRequest {
  tab: PanelTab
  /** A saved message to scroll to and flash once the list is shown. */
  messageId?: string
}

export const pendingPanel = shallowRef<PanelRequest | null>(null)

export function openSavedPanel(request: PanelRequest = { tab: 'saved' }) {
  pendingPanel.value = request
}

/** The tip id under which the one-time "saved messages live here" hint is dismissed (with the member's other tips). */
export const SAVED_HINT_TIP = 'saved-hint'
