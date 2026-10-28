// State and actions of the push section in the App dialog: whether this
// device receives push, the member's other devices, the test button and the
// preview choice.
import { computed, onMounted, ref } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import {
  enablePush, loadConfig, loadDevices, pushAvailability, refreshPushState, removeDevice, sendTestPush, setPreview, thisDevice,
} from '../lib/webPush'
import type { PushConfig, PushSubscription } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { applyPushConfig } from '../lib/notifyPreview'
import { t } from '../i18n'

export function usePushSettings() {
  const toasts = useToastStore()
  const availability = pushAvailability()
  const config = ref<PushConfig | null>(null)
  const devices = ref<PushSubscription[]>([])
  const mine = ref<PushSubscription | null>(null)
  const loading = ref(true)
  const busy = ref(false)
  const failed = ref(false)

  // The permission is a browser property Vue cannot observe: read again after each load.
  const isDenied = () => typeof Notification !== 'undefined' && Notification.permission === 'denied'
  const blocked = ref(isDenied())
  const enabledByOperator = computed(() => config.value?.enabled === true)

  async function load() {
    loading.value = true
    failed.value = false
    try {
      config.value = await loadConfig()
      applyPushConfig(config.value)
      if (config.value.enabled) {
        devices.value = await loadDevices()
        mine.value = availability === 'ready' ? await thisDevice(devices.value) : null
      }
    } catch {
      failed.value = true
    } finally {
      loading.value = false
    }
    blocked.value = isDenied()
    await refreshPushState()
  }

  /** Runs one action with the busy flag and a toast for a failure. */
  async function act(work: () => Promise<void>, failure: string) {
    busy.value = true
    try {
      await work()
    } catch (error) {
      toasts.error(caughtErrorMessage(error, failure))
    } finally {
      busy.value = false
    }
  }

  const enable = () => act(async () => {
    if (!config.value) return
    const result = await enablePush(config.value)
    if (result === 'denied') toasts.error(t('push.blockedToast'))
    else if (result === 'unavailable') toasts.error(t('push.unavailableToast'))
    else toasts.success(t('push.enabledToast'))
    await load()
  }, t('push.enableFailed'))

  const remove = (device: PushSubscription) => act(async () => {
    await removeDevice(device)
    await load()
  }, t('push.removeFailed'))

  const test = () => act(async () => {
    const result = await sendTestPush()
    if (result.delivered > 0) toasts.success(t('push.testSent'))
    else toasts.error(t('push.testFailed'))
    await load()
  }, t('push.testFailed'))

  const setText = (on: boolean) => act(async () => {
    await setPreview(on)
    if (config.value) {
      config.value = { ...config.value, preview: on }
      applyPushConfig(config.value)
    }
  }, t('push.previewFailed'))

  onMounted(load)

  return { availability, config, devices, mine, loading, busy, failed, blocked, enabledByOperator, load, enable, remove, test, setText }
}
