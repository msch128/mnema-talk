<script setup lang="ts">
import type { PropType } from 'vue'
import type { ContextMenuItem, MenuAnchor, MenuItemId, MenuSubmenu } from './menuTypes'
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { ChevronRight } from '@lucide/vue'
import ContextMenuRow from './ContextMenuRow.vue'
import { placeNear, pointBox, type AnchorBox } from '../lib/floatingPosition'

// Context menu. Items: { label, icon, shortcut, action, danger, disabled },
// { type: 'separator' }, { type: 'label', label }, { type: 'slider', ... },
// { type: 'radio', label, subtitle, checked, action },
// { type: 'checkbox', label, checked, action },
// { type: 'info', label, value } (a read-only line),
// { type: 'submenu', id, label, icon, value, items, onOpen, onClose }: opens
// a nested menu to the side (hover, click, ArrowRight; ArrowLeft or Escape
// go back). keepOpen on an item leaves the menu open after choosing it.
// Position either with x/y (viewport px) or with `anchor` (an element or a
// { left, top, right, bottom } rect), which is what keyboard openers use.
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  x: { type: Number, default: 0 },
  y: { type: Number, default: 0 },
  anchor: { type: Object as PropType<MenuAnchor | null>, default: null },
  items: { type: Array as PropType<ContextMenuItem[]>, default: () => [] },
  minWidth: { type: Number, default: 180 },
  ariaLabel: { type: String, default: '' },
  // Where the menu is attached: the page, or the element in full screen.
  teleportTo: { type: [String, Object] as PropType<string | HTMLElement>, default: 'body' }
})

const emit = defineEmits<{ 'update:modelValue': [value: boolean]; close: []; select: [item: ContextMenuItem] }>()

const menuEl = ref<HTMLElement | null>(null)
const posX = ref(props.x)
const posY = ref(props.y)
let opener: HTMLElement | null = null

// The open submenu: its item id, its onClose, where it shows.
const openSub = ref<MenuItemId | null>(null)
let subClose: (() => void) | null = null
const subFlip = ref(false)
const subShift = ref(0)
let hoverTimer: ReturnType<typeof setTimeout> | undefined

// What the menu belongs to: the anchor's box, or the point it was asked at.
function baseBox(): AnchorBox {
  const a = props.anchor
  if (!a) return pointBox(props.x, props.y)
  const r = 'getBoundingClientRect' in a ? a.getBoundingClientRect() : a
  const left = r.left ?? r.x ?? 0
  const top = r.top ?? r.y ?? 0
  return { left, top, right: r.right ?? left, bottom: r.bottom ?? top + (r.height ?? 0) }
}

// Below and left-aligned to the anchor; flipped above and/or right-aligned when
// that would leave the window (see lib/floatingPosition).
function adjustPosition() {
  if (!menuEl.value) return
  const rect = menuEl.value.getBoundingClientRect()
  const box = baseBox()
  // A gap between the menu and what it belongs to; a point has no edge to keep clear of.
  const { x, y } = placeNear(box, rect, { width: window.innerWidth, height: window.innerHeight }, { gap: box.bottom > box.top ? 4 : 0 })
  posX.value = x
  posY.value = y
}

// The menu level the focus is in: the root or an open submenu.
function currentPanel() {
  const a = document.activeElement
  if (a && menuEl.value?.contains(a)) return a.closest<HTMLElement>('[data-menu-panel]') || menuEl.value
  return menuEl.value
}

// Focusable rows of one level in DOM order: menu items plus slider inputs.
function rows(panel: HTMLElement | null = currentPanel()) {
  if (!panel) return []
  return [...panel.querySelectorAll<HTMLElement>('[data-menu-nav]:not([disabled])')]
    .filter(el => el.closest('[data-menu-panel]') === panel)
}

function focusRow(i: number, panel: HTMLElement | null) {
  const list = rows(panel)
  if (!list.length) return
  list[(i + list.length) % list.length]?.focus()
}

function subPanel() {
  return menuEl.value?.querySelector<HTMLElement>('[data-submenu-panel]') || null
}

function triggerOf(id: MenuItemId) {
  return menuEl.value?.querySelector<HTMLElement>(`[data-submenu-trigger="${id}"]`) || null
}

function placeSubmenu() {
  subFlip.value = false
  subShift.value = 0
  nextTick(() => {
    const panel = subPanel()
    if (!panel) return
    const pad = 8
    const r = panel.getBoundingClientRect()
    if (r.right > window.innerWidth - pad) subFlip.value = true
    if (r.bottom > window.innerHeight - pad) subShift.value = -Math.min(r.top - pad, r.bottom - (window.innerHeight - pad))
  })
}

function openSubmenu(item: MenuSubmenu, { focus = false } = {}) {
  clearTimeout(hoverTimer)
  if (item.disabled) return
  if (openSub.value !== item.id) {
    closeSubmenu()
    openSub.value = item.id
    subClose = item.onClose || null
    item.onOpen?.()
    placeSubmenu()
  }
  if (focus) nextTick(() => focusRow(0, subPanel()))
}

function closeSubmenu({ focusTrigger = false } = {}) {
  clearTimeout(hoverTimer)
  const id = openSub.value
  if (id === null) return
  openSub.value = null
  const done = subClose
  subClose = null
  done?.()
  if (focusTrigger) nextTick(() => triggerOf(id)?.focus())
}

// A click opens the submenu (hovering may have opened it already: it stays);
// a keyboard click (Enter, Space) also moves into it.
function onTriggerClick(item: MenuSubmenu, e: MouseEvent) {
  openSubmenu(item, { focus: e.detail === 0 })
}

// Hovering another row of the root closes an open submenu after a moment,
// so a diagonal move into the submenu does not lose it.
function onRootRowHover() {
  if (openSub.value === null) return
  clearTimeout(hoverTimer)
  hoverTimer = setTimeout(() => closeSubmenu(), 250)
}

function cancelHoverClose() {
  clearTimeout(hoverTimer)
}

// A scroll begun just before opening (the row was scrolled into view for the click) arrives a frame late and must not close the new menu.
let settling = false

watch(() => props.modelValue, (open) => {
  if (open) {
    settling = true
    requestAnimationFrame(() => requestAnimationFrame(() => { settling = false }))
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const box = baseBox()
    posX.value = box.left
    posY.value = box.bottom
    nextTick(() => {
      adjustPosition()
      // The container holds focus until an arrow key moves into the items.
      menuEl.value?.focus()
    })
  } else {
    closeSubmenu()
  }
})

watch([() => props.x, () => props.y, () => props.anchor], () => {
  if (props.modelValue) {
    const box = baseBox()
    posX.value = box.left
    posY.value = box.bottom
    nextTick(adjustPosition)
  }
})

function restoreFocus() {
  const el = opener
  opener = null
  if (el && el.isConnected) el.focus?.()
}

function close({ focus = false } = {}) {
  closeSubmenu()
  emit('update:modelValue', false)
  emit('close')
  if (focus) nextTick(restoreFocus)
}

function handleSelect(item: ContextMenuItem) {
  if (item.disabled) return
  if (item.action) item.action()
  emit('select', item)
  // Radio groups and checkboxes may stay open to show the result.
  if (item.keepOpen) return
  closeSubmenu()
  emit('update:modelValue', false)
  emit('close')
  // Return focus to the opener unless the action moved it somewhere else.
  nextTick(() => {
    const a = document.activeElement
    if (!a || a === document.body) restoreFocus()
    else opener = null
  })
}

function handleKeydown(e: KeyboardEvent) {
  const panel = currentPanel()
  if (handleLeaveKey(e, panel) || handleSubmenuKey(e)) return
  const list = rows(panel)
  if (!list.length) return
  const cur = list.findIndex(el => el === document.activeElement)
  if (!handleRowKey(e, panel, cur)) typeAhead(e, list, cur)
}

// Escape, Tab and ArrowLeft leave the menu or the open submenu.
function handleLeaveKey(e: KeyboardEvent, panel: HTMLElement | null): boolean {
  const inSub = !!panel && panel !== menuEl.value
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    if (inSub) closeSubmenu({ focusTrigger: true })
    else close({ focus: true })
    return true
  }
  if (e.key === 'Tab') {
    // Leave the menu; focus continues from the opener.
    close({ focus: true })
    return true
  }
  if (e.key === 'ArrowLeft' && inSub) {
    e.preventDefault()
    closeSubmenu({ focusTrigger: true })
    return true
  }
  return false
}

function handleSubmenuKey(e: KeyboardEvent): boolean {
  const trigger = document.activeElement?.closest?.('[data-submenu-trigger]')
  if (e.key !== 'ArrowRight' || !trigger || !menuEl.value?.contains(trigger)) return false
  e.preventDefault()
  const item = props.items.find(i => i.type === 'submenu' && String(i.id) === trigger.getAttribute('data-submenu-trigger'))
  if (item?.type === 'submenu') openSubmenu(item, { focus: true })
  return true
}

function handleRowKey(e: KeyboardEvent, panel: HTMLElement | null, cur: number): boolean {
  const target = rowTarget(e.key, cur)
  if (target === null) return false
  e.preventDefault()
  focusRow(target, panel)
  return true
}

function rowTarget(key: string, cur: number): number | null {
  if (key === 'ArrowDown') return cur < 0 ? 0 : cur + 1
  if (key === 'ArrowUp') return cur < 0 ? -1 : cur - 1
  if (key === 'Home') return 0
  if (key === 'End') return -1
  return null
}

function isPrintableCharKey(e: KeyboardEvent): boolean {
  if (e.ctrlKey || e.metaKey || e.altKey) return false
  if (e.key.length !== 1) return false
  return /\S/.test(e.key)
}

// Type-ahead: next row whose label starts with the typed letter.
function typeAhead(e: KeyboardEvent, list: HTMLElement[], cur: number) {
  if (!isPrintableCharKey(e)) return
  const ch = e.key.toLowerCase()
  const n = list.length
  for (let k = 1; k <= n; k++) {
    const el = list[(Math.max(cur, -1) + k) % n]
    if (el?.textContent?.trim().toLowerCase().startsWith(ch)) {
      el.focus()
      break
    }
  }
}

function isInsideMenu(target: EventTarget | null): boolean {
  return target instanceof Node && !!menuEl.value?.contains(target)
}

// The anchor element is the opener's button: its own click toggles the menu,
// so a press on it must not also close the menu first (it would reopen).
function isOnAnchor(target: EventTarget | null): boolean {
  const a = props.anchor
  return a instanceof Node && target instanceof Node && a.contains(target)
}

function handleOutsideDismiss(e: Event) {
  if (!props.modelValue || !menuEl.value) return
  if (!isInsideMenu(e.target) && !isOnAnchor(e.target)) {
    opener = null
    close()
  }
}

const handleScrollDismiss = (e: Event) => { if (!settling) handleOutsideDismiss(e) }
onMounted(() => {
  document.addEventListener('mousedown', handleOutsideDismiss, true)
  document.addEventListener('contextmenu', handleOutsideDismiss, true)
  window.addEventListener('scroll', handleScrollDismiss, true)
})

onUnmounted(() => {
  closeSubmenu()
  document.removeEventListener('mousedown', handleOutsideDismiss, true)
  document.removeEventListener('contextmenu', handleOutsideDismiss, true)
  window.removeEventListener('scroll', handleScrollDismiss, true)
})

const panelClass = 'p-2 rounded-xl bg-mnema-elevated border border-mnema-border shadow-lg text-sm text-mnema-muted font-sans'
</script>

<template>
  <Teleport :to="teleportTo">
    <div
      v-if="modelValue"
      ref="menuEl"
      role="menu"
      data-menu-panel
      :aria-label="ariaLabel || undefined"
      tabindex="-1"
      :style="{
        left: `${posX}px`,
        top: `${posY}px`,
        minWidth: `${minWidth}px`
      }"
      :class="['fixed z-50 focus:outline-hidden select-none pop-in', panelClass]"
      @keydown="handleKeydown"
    >
      <slot name="header"></slot>

      <template v-for="(item, idx) in items" :key="item.id ?? idx">
        <div
          v-if="item.type === 'submenu'"
          class="relative"
          @mouseenter="openSubmenu(item)"
        >
          <button
            type="button"
            role="menuitem"
            aria-haspopup="menu"
            :aria-expanded="openSub === item.id ? 'true' : 'false'"
            :aria-disabled="item.disabled ? 'true' : undefined"
            :data-submenu-trigger="item.id"
            data-menu-nav
            tabindex="-1"
            :disabled="item.disabled"
            :class="[
              'w-full h-9 px-3 flex items-center justify-between gap-3 rounded-lg text-left transition-colors font-normal focus:outline-hidden',
              item.disabled ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer hover:bg-mnema-hover hover:text-mnema-text focus-visible:bg-mnema-hover focus-visible:text-mnema-text',
              openSub === item.id ? 'bg-mnema-hover text-mnema-text' : ''
            ]"
            @click="onTriggerClick(item, $event)"
          >
            <div class="flex items-center gap-2 truncate">
              <component :is="item.icon" v-if="item.icon" class="w-4 h-4 shrink-0 opacity-80" />
              <span class="truncate">{{ item.label }}</span>
            </div>
            <span class="ml-auto flex items-center gap-1 text-xs text-mnema-tertiary shrink-0">
              <span v-if="item.value">{{ item.value }}</span>
              <ChevronRight class="w-3.5 h-3.5" aria-hidden="true" />
            </span>
          </button>

          <div
            v-if="openSub === item.id"
            role="menu"
            data-menu-panel
            data-submenu-panel
            :data-submenu="item.id"
            :aria-label="item.label"
            :style="{ top: `${subShift - 6}px`, minWidth: `${item.minWidth || 180}px` }"
            :class="['absolute z-50', subFlip ? 'right-full mr-1.5' : 'left-full ml-1.5', panelClass]"
            @mouseenter="cancelHoverClose"
          >
            <ContextMenuRow
              v-for="(sub, sIdx) in (item.items || [])"
              :key="sub.id ?? sIdx"
              :item="sub"
              @select="handleSelect"
            />
          </div>
        </div>

        <ContextMenuRow
          v-else
          :item="item"
          @select="handleSelect"
          @mouseenter="onRootRowHover"
        />
      </template>

      <slot name="footer"></slot>
    </div>
  </Teleport>
</template>
