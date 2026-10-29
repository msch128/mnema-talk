import { onActivated } from 'vue'

/**
 * Runs `reload` each time a kept-alive component comes back into view, but not
 * on the first activation, which follows the mount that loaded it already.
 */
export function useReloadOnActivate(reload: () => unknown): void {
  let first = true
  onActivated(() => {
    if (first) first = false
    else void reload()
  })
}
