// Tells the multi-instance desktop client whether this page is busy: it closes
// the page of a server that stayed hidden for a while to save memory, and must
// not do that while closing would lose something: text typed and not written
// yet (or a draft storage refused; stored drafts come back), a file in a
// composer's tray (waiting, uploading or uploaded and not sent), or a Talk.
import { computed, watch } from 'vue'
import { platform } from '../../shared/lib/platform'
import { useVoiceStore } from '../../stores/voice'
import { draftsAtRisk } from '../../chat/lib/drafts'
import { anyAttachmentHeld } from '../../chat/composables/attachmentTrayState'

export function useDesktopBusy() {
  const voice = useVoiceStore()
  const draft = computed(draftsAtRisk)
  const upload = computed(anyAttachmentHeld)
  const talk = computed(() => voice.isConnected)
  // Three booleans, so a change of any (and nothing else) reaches the host.
  watch([draft, upload, talk], ([hasDraft, uploading, inTalk]) => {
    if (platform.supportsBusyReport()) platform.reportBusy({ draft: hasDraft, upload: uploading, talk: inTalk })
  }, { immediate: true })
}
