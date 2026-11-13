// Tells the multi-instance desktop client whether this page is busy: it closes
// the page of a server that stayed hidden for a while to save memory, and must
// not do that while the member has unsent text, a running upload or a Talk.
import { computed, watch } from 'vue'
import { platform } from '../../shared/lib/platform'
import { useVoiceStore } from '../../stores/voice'
import { hasAnyDraft } from '../../chat/lib/drafts'
import { anyUploadPending } from '../../chat/composables/attachmentTrayState'

export function useDesktopBusy() {
  const voice = useVoiceStore()
  const draft = computed(hasAnyDraft)
  const upload = computed(anyUploadPending)
  const talk = computed(() => voice.isConnected)
  // Three booleans, so a change of any (and nothing else) reaches the host.
  watch([draft, upload, talk], ([hasDraft, uploading, inTalk]) => {
    if (platform.supportsBusyReport()) platform.reportBusy({ draft: hasDraft, upload: uploading, talk: inTalk })
  }, { immediate: true })
}
