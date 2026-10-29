// One way to open a context menu on any element: right click, the Menu key or
// Shift+F10 (both arrive as `contextmenu`), and a long press with a finger.
// Bind `trigger.on` to the element with v-bind and render a ContextMenu from
// `trigger.menu` (see useMenuState).
import { onBeforeUnmount, getCurrentInstance } from 'vue'
import type { ContextMenuItem } from '../components/menuTypes'
import { useMenuState } from './useNavMenus'
import type { MenuTrigger } from '../lib/contextMenuPoint'

export const LONG_PRESS_MS = 500
// A finger that moves further than this is scrolling, not pressing.
const LONG_PRESS_SLOP = 10

interface TriggerOptions {
  /** Runs when the menu is asked for, e.g. to refresh a device list. */
  onOpen?: () => void
  /** A menu that has nothing to offer right now (not sharing yet) leaves the event alone. */
  enabled?: () => boolean
}

export function useContextMenuTrigger(build: () => ContextMenuItem[], { onOpen, enabled }: TriggerOptions = {}) {
  const menu = useMenuState()
  let timer: ReturnType<typeof setTimeout> | undefined
  let origin = { x: 0, y: 0 }
  // The finger's long press already opened the menu: swallow the click that follows its release.
  let opened = false

  function cancel() {
    clearTimeout(timer)
    timer = undefined
  }

  function open(e: MenuTrigger) {
    if (enabled && !enabled()) return
    onOpen?.()
    return menu.show(e, build)
  }

  function onContextmenu(e: MouseEvent) {
    // Some touch browsers send their own contextmenu after the long press that opened the menu.
    if (opened) {
      e.preventDefault()
      return
    }
    void open(e)
  }

  function onPointerdown(e: PointerEvent) {
    if (e.pointerType !== 'touch' || !e.isPrimary) return
    cancel()
    opened = false
    origin = { x: e.clientX, y: e.clientY }
    const currentTarget = e.currentTarget
    timer = setTimeout(() => {
      timer = undefined
      opened = true
      void open({ clientX: origin.x, clientY: origin.y, currentTarget })
    }, LONG_PRESS_MS)
  }

  // The click of a released long press arrives right away; if none comes, the flag must not outlive it.
  function onPointerup() {
    cancel()
    if (opened) setTimeout(() => { opened = false }, 400)
  }

  function onPointermove(e: PointerEvent) {
    if (timer === undefined) return
    if (Math.hypot(e.clientX - origin.x, e.clientY - origin.y) > LONG_PRESS_SLOP) cancel()
  }

  // The release after a long press must not also press the button.
  function onClickCapture(e: MouseEvent) {
    if (!opened) return
    opened = false
    e.preventDefault()
    e.stopPropagation()
  }

  if (getCurrentInstance()) onBeforeUnmount(cancel)

  function close() {
    menu.state.open = false
  }

  const on = {
    onContextmenu,
    onPointerdown,
    onPointermove,
    onPointerup,
    onPointercancel: cancel,
    onPointerleave: cancel,
    onClickCapture
  }
  return { menu, on, close }
}
