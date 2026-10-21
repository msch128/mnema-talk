// The "enable desktop notifications" hint above the message list and the
// per-channel notification level menu entries.
import { ref, computed } from 'vue'
import { notifyMenuItems } from './useMuteMenu'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const NOTIF_HINT_KEY = 'mnema_notif_hint_dismissed'

function readHintDismissed() {
  try { return localStorage.getItem(NOTIF_HINT_KEY) === '1' } catch (err) { void err; return false }
}

export function useNotificationHint(isPanel: () => boolean) {
  const chatStore = useChatStore()
  const toasts = useToastStore()

  // The desktop client's system may not have been asked yet (macOS asks on the first toast or on the click below).
  void chatStore.refreshNotificationPermission()

  const notifHintDismissed = ref(readHintDismissed())
  const showNotifHint = computed(() =>
    !isPanel() && chatStore.notificationPermission === 'default' && !notifHintDismissed.value
  )

  function dismissNotifHint() {
    notifHintDismissed.value = true
    try { localStorage.setItem(NOTIF_HINT_KEY, '1') } catch (err) { void err }
  }

  // Asked on a click, never on page load.
  async function enableNotifications() {
    const result = await chatStore.requestNotificationPermission()
    if (result === 'granted') toasts.success(t('notifications.granted'))
    else if (result === 'denied') toasts.error(t('notifications.denied'))
    if (result !== 'default') dismissNotifHint()
  }

  // Menu rows for the channel's notification level and mutes.
  const notificationMenuItems = (channelId: string) => notifyMenuItems({ kind: 'channel', id: channelId })

  return { showNotifHint, dismissNotifHint, enableNotifications, notificationMenuItems }
}
