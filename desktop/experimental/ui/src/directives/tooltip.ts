import type { DirectiveBinding, ObjectDirective } from 'vue'

export type TooltipValue = string | { text: string; shortcut?: string } | null | undefined
interface TooltipData { text: string; shortcut: string }
interface TooltipHandlers {
  enter: (event: PointerEvent) => void
  leave: () => void
  focus: () => void
  key: (event: KeyboardEvent) => void
}
export interface TooltipElement extends HTMLElement {
  __tip?: TooltipData | null
  __tipLabelSet?: boolean
  __tipId?: number
  __tipHandlers?: TooltipHandlers
}

// v-tooltip="'Antworten'" or v-tooltip="{ text: 'Stummschalten', shortcut: 'M' }"
// One shared floating element. Shows after 400 ms of hover or at once on
// keyboard focus, hides on leave/blur/Escape/press. By default the directive
// also sets aria-label to the same text (icon buttons); use the .visual
// modifier for tooltips on visible text (e.g. truncated names) to leave the
// accessible name alone.

const HOVER_DELAY = 400
const GAP = 6

let tipEl: HTMLDivElement | null = null
let showTimer: ReturnType<typeof setTimeout> | undefined
let currentTarget: TooltipElement | null = null
let seq = 0

function ensureTip() {
  if (tipEl) {
    if (!tipEl.isConnected) document.body.appendChild(tipEl)
    return tipEl
  }
  tipEl = document.createElement('div')
  tipEl.setAttribute('role', 'tooltip')
  tipEl.className = 'mnema-tooltip'
  tipEl.id = 'mnema-tooltip'
  tipEl.hidden = true
  document.body.appendChild(tipEl)
  return tipEl
}

function normalize(value: TooltipValue): TooltipData | null {
  if (!value) return null
  if (typeof value === 'string') return { text: value, shortcut: '' }
  if (!value.text) return null
  return { text: String(value.text), shortcut: value.shortcut || '' }
}

function position(target: HTMLElement) {
  const tip = tipEl
  if (!tip) return
  const r = target.getBoundingClientRect()
  const tw = tip.offsetWidth
  const th = tip.offsetHeight
  let left = r.left + r.width / 2 - tw / 2
  left = Math.max(8, Math.min(left, window.innerWidth - tw - 8))
  let top = r.top - th - GAP
  if (top < 8) top = r.bottom + GAP // no room above: place below
  tip.style.left = `${Math.round(left)}px`
  tip.style.top = `${Math.round(top)}px`
}

export function showTip(target: TooltipElement, immediate = false) {
  const data = target.__tip
  if (!data) return
  clearTimeout(showTimer)
  const run = () => {
    if (!target.isConnected) return
    const tip = ensureTip()
    tip.textContent = ''
    const label = document.createElement('span')
    label.textContent = data.text
    tip.appendChild(label)
    if (data.shortcut) {
      const kbd = document.createElement('span')
      kbd.className = 'mnema-tooltip-kbd'
      kbd.textContent = data.shortcut
      tip.appendChild(kbd)
    }
    tip.hidden = false
    currentTarget = target
    target.setAttribute('aria-describedby', tip.id)
    position(target)
  }
  if (immediate) run()
  else showTimer = setTimeout(run, HOVER_DELAY)
}

export function hideTip(target?: TooltipElement) {
  clearTimeout(showTimer)
  if (target && currentTarget && target !== currentTarget) return
  if (tipEl) tipEl.hidden = true
  currentTarget?.removeAttribute('aria-describedby')
  currentTarget = null
}

function setup(el: TooltipElement, binding: DirectiveBinding<TooltipValue>) {
  const data = normalize(binding.value)
  el.__tip = data
  const visual = binding.modifiers?.visual
  if (!visual) {
    if (data) el.setAttribute('aria-label', data.text)
    else if (el.__tipLabelSet) el.removeAttribute('aria-label')
    el.__tipLabelSet = !!data
  }
  // Text changed while shown (toggle buttons): refresh in place.
  if (currentTarget === el) {
    if (data) showTip(el, true)
    else hideTip(el)
  }
}

export const tooltip: ObjectDirective<TooltipElement, TooltipValue> = {
  mounted(el, binding) {
    el.__tipId = ++seq
    setup(el, binding)
    el.__tipHandlers = {
      enter: e => { if (e.pointerType !== 'touch') showTip(el) },
      leave: () => hideTip(el),
      focus: () => {
        // Keyboard focus only; mouse clicks on buttons also focus them.
        try { if (!el.matches(':focus-visible')) return } catch { /* old engine */ }
        showTip(el, true)
      },
      key: e => { if (e.key === 'Escape') hideTip(el) }
    }
    const h = el.__tipHandlers
    el.addEventListener('pointerenter', h.enter)
    el.addEventListener('pointerleave', h.leave)
    el.addEventListener('pointerdown', h.leave)
    el.addEventListener('focus', h.focus)
    el.addEventListener('blur', h.leave)
    el.addEventListener('keydown', h.key)
  },
  updated(el, binding) {
    setup(el, binding)
  },
  beforeUnmount(el) {
    hideTip(el)
    const h = el.__tipHandlers
    if (!h) return
    el.removeEventListener('pointerenter', h.enter)
    el.removeEventListener('pointerleave', h.leave)
    el.removeEventListener('pointerdown', h.leave)
    el.removeEventListener('focus', h.focus)
    el.removeEventListener('blur', h.leave)
    el.removeEventListener('keydown', h.key)
  }
}
