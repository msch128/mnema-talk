// The "enable desktop notifications" hint above the message list and the
// per-channel notification level menu entries.
import { ref, computed, type Component } from 'vue'
import { Bell, BellOff, AtSign } from '@lucide/vue'
import type { NotifyLevel } from '../types/domain'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const NOTIF_HINT_KEY = 'mnema_notif_hint_dismissed'

function readHintDismissed() {
  try { return localStorage.getItem(NOTIF_HINT_KEY) === '1' } catch { return false }
}

export function useNotificationHint(isPanel: () => boolean) {
  const chatStore = useChatStore()
  const toasts = useToastStore()

  const notifHintDismissed = ref(readHintDismissed())
  const showNotifHint = computed(() =>
    !isPanel() && chatStore.notificationPermission === 'default' && !notifHintDismissed.value
  )

  function dismissNotifHint() {
    notifHintDismissed.value = true
    try { localStorage.setItem(NOTIF_HINT_KEY, '1') } catch { /* private mode: dismissed for this session only */ }
  }

  // Asked on a click, never on page load.
  async function enableNotifications() {
    const result = await chatStore.requestNotificationPermission()
    if (result === 'granted') toasts.success(t('notifications.granted'))
    else if (result === 'denied') toasts.error(t('notifications.denied'))
    if (result !== 'default') dismissNotifHint()
  }

  // Menu rows for the channel's notification level.
  function notificationMenuItems(channelId: string) {
    const current = chatStore.notificationLevel(channelId)
    const levels: { level: NotifyLevel; icon: Component }[] = [
      { level: 'all', icon: Bell },
      { level: 'mentions', icon: AtSign },
      { level: 'mute', icon: BellOff }
    ]
    return levels.map(({ level, icon }) => ({
      label: t(`notifications.${level}`),
      icon,
      shortcut: current === level ? '✓' : '',
      action: () => chatStore.setNotificationLevel(channelId, level)
    }))
  }

  return { showNotifHint, dismissNotifHint, enableNotifications, notificationMenuItems }
}
