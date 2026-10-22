// The "enable desktop notifications" hint above the message list and the
// per-channel notification level menu entries.
import { computed, onMounted } from 'vue'
import { notifyMenuItems } from './useMuteMenu'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useOnboardingStore } from '../stores/onboarding'
import { NOTIFICATION_HINT_TIP } from '../lib/onboardingTips'
import { t } from '../i18n'

export function useNotificationHint(isPanel: () => boolean) {
  const chatStore = useChatStore()
  const toasts = useToastStore()
  const onboarding = useOnboardingStore()
  onMounted(() => { void onboarding.ensureLoaded() })

  // The desktop client's system may not have been asked yet (macOS asks on the first toast or on the click below).
  void chatStore.refreshNotificationPermission()

  // Dismissed on the server with the member's other tips; nothing shows before it is known.
  const showNotifHint = computed(() =>
    !isPanel() && chatStore.notificationPermission === 'default' &&
    !onboarding.welcomeOpen && !!onboarding.state && !onboarding.state.dismissed_tips.includes(NOTIFICATION_HINT_TIP)
  )

  const dismissNotifHint = () => onboarding.dismiss(NOTIFICATION_HINT_TIP)

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
