<script setup>
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { ChevronRight } from '@lucide/vue'
import ContextMenuRow from './ContextMenuRow.vue'

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
  anchor: { type: [Object, null], default: null },
  items: { type: Array, default: () => [] },
  minWidth: { type: Number, default: 180 },
  ariaLabel: { type: String, default: '' }
})

const emit = defineEmits(['update:modelValue', 'close', 'select'])

const menuEl = ref(null)
const posX = ref(props.x)
const posY = ref(props.y)
let opener = null

// The open submenu: its item id, its onClose, where it shows.
const openSub = ref(null)
let subClose = null
const subFlip = ref(false)
const subShift = ref(0)
let hoverTimer = null

function basePoint() {
  const a = props.anchor
  if (a) {
    const r = typeof a.getBoundingClientRect === 'function' ? a.getBoundingClientRect() : a
    const left = r.left ?? r.x ?? 0
    const bottom = r.bottom ?? ((r.top ?? r.y ?? 0) + (r.height ?? 0))
    return { x: left, y: bottom }
  }
  return { x: props.x, y: props.y }
}

function adjustPosition() {
  if (!menuEl.value) return
  const rect = menuEl.value.getBoundingClientRect()
  const pad = 8
  const { x, y } = basePoint()
  let newX = x
  let newY = y
  if (newX + rect.width > window.innerWidth - pad) newX = Math.max(pad, window.innerWidth - rect.width - pad)
  if (newY + rect.height > window.innerHeight - pad) newY = Math.max(pad, window.innerHeight - rect.height - pad)
  posX.value = newX
  posY.value = newY
}

// The menu level the focus is in: the root or an open submenu.
function currentPanel() {
  const a = document.activeElement
  if (a && menuEl.value?.contains(a)) return a.closest('[data-menu-panel]') || menuEl.value
  return menuEl.value
}

// Focusable rows of one level in DOM order: menu items plus slider inputs.
function rows(panel = currentPanel()) {
  if (!panel) return []
  return [...panel.querySelectorAll('[data-menu-nav]:not([disabled])')]
    .filter(el => el.closest('[data-menu-panel]') === panel)
}

function focusRow(i, panel) {
  const list = rows(panel)
  if (!list.length) return
  list[(i + list.length) % list.length]?.focus()
}

function subPanel() {
  return menuEl.value?.querySelector('[data-submenu-panel]') || null
}

function triggerOf(id) {
  return menuEl.value?.querySelector(`[data-submenu-trigger="${id}"]`) || null
}

function placeSubmenu() {
  subFlip.value = false
  subShift.value = 0
  nextTick(() => {
    const panel = subPanel()
    if (!panel) return
    const pad = 8
    let r = panel.getBoundingClientRect()
    if (r.right > window.innerWidth - pad) subFlip.value = true
    if (r.bottom > window.innerHeight - pad) subShift.value = -Math.min(r.top - pad, r.bottom - (window.innerHeight - pad))
  })
}

function openSubmenu(item, { focus = false } = {}) {
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
function onTriggerClick(item, e) {
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

watch(() => props.modelValue, (open) => {
  if (open) {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const p = basePoint()
    posX.value = p.x
    posY.value = p.y
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
    const p = basePoint()
    posX.value = p.x
    posY.value = p.y
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

function handleSelect(item) {
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

function handleKeydown(e) {
  const panel = currentPanel()
  const inSub = !!panel && panel !== menuEl.value
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    if (inSub) closeSubmenu({ focusTrigger: true })
    else close({ focus: true })
    return
  }
  if (e.key === 'Tab') {
    // Leave the menu; focus continues from the opener.
    close({ focus: true })
    return
  }
  if (e.key === 'ArrowLeft' && inSub) {
    e.preventDefault()
    closeSubmenu({ focusTrigger: true })
    return
  }
  const trigger = document.activeElement?.closest?.('[data-submenu-trigger]')
  if (e.key === 'ArrowRight' && trigger && menuEl.value?.contains(trigger)) {
    e.preventDefault()
    const item = props.items.find(i => i.type === 'submenu' && String(i.id) === trigger.getAttribute('data-submenu-trigger'))
    if (item) openSubmenu(item, { focus: true })
    return
  }

  const list = rows(panel)
  if (!list.length) return
  const cur = list.indexOf(document.activeElement)

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    focusRow(cur < 0 ? 0 : cur + 1, panel)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    focusRow(cur < 0 ? -1 : cur - 1, panel)
  } else if (e.key === 'Home') {
    e.preventDefault()
    focusRow(0, panel)
  } else if (e.key === 'End') {
    e.preventDefault()
    focusRow(-1, panel)
  } else if (e.key.length === 1 && /\S/.test(e.key) && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // Type-ahead: next row whose label starts with the typed letter.
    const ch = e.key.toLowerCase()
    const n = list.length
    for (let k = 1; k <= n; k++) {
      const el = list[(Math.max(cur, -1) + k) % n]
      if (el.textContent.trim().toLowerCase().startsWith(ch)) {
        el.focus()
        break
      }
    }
  }
}

function handleClickOutside(e) {
  if (props.modelValue && menuEl.value && !menuEl.value.contains(e.target)) {
    opener = null
    close()
  }
}

function handleScroll(e) {
  if (props.modelValue && !(menuEl.value && menuEl.value.contains(e.target))) {
    opener = null
    close()
  }
}

onMounted(() => {
  document.addEventListener('mousedown', handleClickOutside, true)
  document.addEventListener('contextmenu', handleClickOutside, true)
  window.addEventListener('scroll', handleScroll, true)
})

onUnmounted(() => {
  closeSubmenu()
  document.removeEventListener('mousedown', handleClickOutside, true)
  document.removeEventListener('contextmenu', handleClickOutside, true)
  window.removeEventListener('scroll', handleScroll, true)
})

const panelClass = 'p-1.5 rounded-lg bg-mnema-elevated border border-mnema-border shadow-2xl text-sm text-mnema-muted font-sans'
</script>

<template>
  <Teleport to="body">
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
      :class="['fixed z-50 focus:outline-none select-none animate-in fade-in zoom-in-95 duration-100', panelClass]"
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
              'w-full h-8 px-2.5 flex items-center justify-between gap-3 rounded text-left transition-colors font-normal focus:outline-none',
              item.disabled ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer hover:bg-mnema-hover hover:text-mnema-text focus-visible:bg-mnema-hover focus-visible:text-mnema-text',
              openSub === item.id ? 'bg-mnema-hover text-mnema-text' : ''
            ]"
            @click="onTriggerClick(item, $event)"
          >
            <div class="flex items-center gap-2 truncate">
              <component :is="item.icon" v-if="item.icon" class="w-4 h-4 flex-shrink-0 opacity-80" />
              <span class="truncate">{{ item.label }}</span>
            </div>
            <span class="ml-auto flex items-center gap-1 text-xs text-mnema-tertiary flex-shrink-0">
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
