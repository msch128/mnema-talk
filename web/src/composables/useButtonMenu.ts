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

  function toggle() {
    if (wasOpen || menu.state.open) {
      wasOpen = false
      menu.state.open = false
      return
    }
    menu.show({ currentTarget: button.value }, build)
  }

  return { menu, button, onPointerDown, toggle }
}
