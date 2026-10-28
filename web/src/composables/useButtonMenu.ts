import { ref } from 'vue'
import { useMenuState } from './useNavMenus'
import type { ContextMenuItem } from '../components/menuTypes'

/**
 * A menu that opens under a button and closes with it. The menu closes on
 * pointer down outside it (also on this button): a click on the button while
 * it was open only closes it.
 */
export function useButtonMenu(build: () => ContextMenuItem[]) {
  const menu = useMenuState()
  const button = ref<HTMLButtonElement | null>(null)
  let wasOpen = false

  function onPointerDown() {
    wasOpen = menu.state.open
  }

  // A keyboard activation (click without a pointer, detail 0) never follows a pointer down; a flag left
  // behind by a press that was released elsewhere must not swallow it.
  function toggle(e?: MouseEvent) {
    if (e && e.detail === 0) wasOpen = false
    if (wasOpen || menu.state.open) {
      wasOpen = false
      menu.state.open = false
      return
    }
    menu.show({ currentTarget: button.value }, build)
  }

  return { menu, button, onPointerDown, toggle }
}
