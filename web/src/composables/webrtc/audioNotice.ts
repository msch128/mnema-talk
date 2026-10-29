import { t } from '../../i18n'
import { useToastStore } from '../../stores/toast'

const shown = new Set<string>()

/** Tells the user once per page load, so a browser that cannot do something does not say so on every join. */
export function noticeOnce(key: string) {
  if (shown.has(key)) return
  shown.add(key)
  useToastStore().info(t(key))
}

/** For tests. */
export function resetNotices() {
  shown.clear()
}
