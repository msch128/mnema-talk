// Moving channels and categories in the sidebar (admins): drag and drop
// measured on the rendered list, Alt+ArrowUp / Alt+ArrowDown, and the
// feedback around a move (highlight, screen reader announcement, scrolling
// into view). Every move goes through `commit` from useChannelLayout, so it
// shows at once, is saved right away and can be undone from a toast.
//
// The rules where something lands live in lib/channelLayout; this file only
// turns pointer positions into the "hits" those rules take.
import { ref, computed, watch, nextTick, useId, getCurrentScope, onScopeDispose } from 'vue'
import type { Ref, ComputedRef } from 'vue'
import type { Category, Channel } from '../types/domain'
import type { ChannelTree } from '../lib/channelTree'
import type { DropHit, DropTarget } from '../lib/channelLayout'
import type { useChannelLayout } from './useChannelLayout'

type ItemKind = 'channel' | 'category'
type SidebarItem = { kind: 'channel'; id: string; name: string; type: Channel['type'] } | { kind: 'category'; id: string; name: string }
interface SidebarTarget extends DropTarget { overHeader?: boolean }
type SidebarTree = ChannelTree<Channel, Category>
interface ReorderOptions { layout: ComputedRef<SidebarTree>; commit: ReturnType<typeof useChannelLayout>['commit']; collapsed: Ref<Set<string>>; expandCategory(id: string): void; enabled(): boolean; onLongPress?: (kind: ItemKind, entity: Channel | Category, event: PointerEvent) => void }
import {
  locateChannel, locateCategory, moveChannel, moveCategory, stepChannel, stepCategory,
  resolveChannelDrop, resolveCategoryDrop
} from '../lib/channelLayout'
import { useSortableDrag } from './useSortableDrag'
import { t } from '../i18n'

// data-drop-section value of the headless uncategorized group.
export const UNCATEGORIZED_SECTION = '__uncategorized'
// A collapsed category opens while a channel is held over it this long.
export const PEEK_MS = 600
const FLASH_MS = 1200

function prefersReducedMotion() {
  return !!globalThis.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches
}

/**
 * options:
 *   layout, commit                 from useChannelLayout
 *   collapsed                      ref(Set) of collapsed category ids
 *   expandCategory(id)             opens a category for good
 *   enabled                        getter: may this user move things?
 *   onLongPress(kind, entity, e)   touch: held and let go without moving
 */
export function useSidebarReorder({ layout, commit, collapsed, expandCategory, enabled, onLongPress }: ReorderOptions) {
  // The scrolling channel list (bind with ref="navEl").
  const navEl = ref<HTMLElement | null>(null)
  const hintId = useId()
  const flashKey = ref('')
  const announcement = ref('')
  // Collapsed categories opened for the moment while a channel is held over them.
  const peeked = ref(new Set<string>())
  let flashTimer: ReturnType<typeof setTimeout> | undefined
  let peekTimer: ReturnType<typeof setTimeout> | undefined
  let peekFor: string | null = null

  /** Whether a category's channels are hidden right now. */
  function isCollapsed(id: string) {
    return collapsed.value.has(id) && !peeked.value.has(id)
  }

  function itemOf(entity: Channel | Category): SidebarItem {
    return 'type' in entity
      ? { kind: 'channel', id: entity.id, name: entity.name, type: entity.type }
      : { kind: 'category', id: entity.id, name: entity.name }
  }

  function categoryLabel(categoryId: string | null) {
    return categoryId == null ? t('admin.uncategorized') : locateCategory(layout.value, categoryId)?.category.name || ''
  }

  // Tells screen readers where the item ended up (the reset makes an
  // identical message count as new).
  function announce(text: string) {
    announcement.value = ''
    nextTick(() => { announcement.value = text })
  }

  function flash(key: string) {
    clearTimeout(flashTimer)
    flashKey.value = ''
    nextTick(() => {
      flashKey.value = key
      flashTimer = setTimeout(() => { flashKey.value = '' }, FLASH_MS)
    })
  }

  function elementOf(kind: ItemKind, id: string) {
    const selector = kind === 'channel' ? `[data-drop="channel"][data-id="${id}"]` : `[data-category-toggle="${id}"]`
    return navEl.value?.querySelector<HTMLElement>(selector) ?? null
  }

  /** Scrolls a channel or category into view and highlights it, opening its category if needed. */
  function reveal(kind: ItemKind, id: string) {
    if (kind === 'channel') {
      const at = locateChannel(layout.value, id)
      if (at?.categoryId) expandCategory(at.categoryId)
    }
    flash(`${kind}:${id}`)
    nextTick(() => {
      elementOf(kind, id)?.scrollIntoView?.({ block: 'nearest', behavior: prefersReducedMotion() ? 'auto' : 'smooth' })
    })
  }

  function applyMove(item: SidebarItem, next: SidebarTree) {
    if (next === layout.value) return
    commit(next, { toast: t(item.kind === 'channel' ? 'sidebar.channelMoved' : 'sidebar.categoryMoved') })
    if (item.kind === 'channel') {
      const at = locateChannel(next, item.id)
      if (!at) return
      announce(t('sidebar.movedChannel', { name: item.name, position: at.index + 1, total: at.total, category: categoryLabel(at.categoryId) }))
    } else {
      const at = locateCategory(next, item.id)
      if (!at) return
      announce(t('sidebar.movedCategory', { name: item.name, position: at.index + 1, total: at.total }))
    }
    flash(`${item.kind}:${item.id}`)
  }

  // ---- Keyboard ----

  /**
   * Alt+ArrowUp / Alt+ArrowDown on a focused row or category header. Channels
   * cross into the neighbouring container at either end; a collapsed
   * category they move into opens, so the row stays visible and keeps focus.
   */
  function moveByKey(kind: ItemKind, entity: Channel | Category, dir: number) {
    if (!enabled() || dragItem.value) return
    const tree = layout.value
    const next = kind === 'channel' ? stepChannel(tree, entity.id, dir) : stepCategory(tree, entity.id, dir)
    if (next === tree) {
      announce(t(dir < 0 ? 'sidebar.atTop' : 'sidebar.atBottom', { name: entity.name }))
      return
    }
    if (kind === 'channel') {
      const at = locateChannel(next, entity.id)
      if (at?.categoryId) expandCategory(at.categoryId)
    }
    applyMove(itemOf(entity), next)
    // Moving to another category re-creates the row: focus it again.
    nextTick(() => {
      const el = elementOf(kind, entity.id)
      el?.focus()
      el?.scrollIntoView?.({ block: 'nearest' })
    })
  }

  // ---- Drag and drop: what the pointer is over ----
  //
  // Only the height counts, so the full width of a row is a target and the
  // gaps between rows don't make the indicator jump.

  function ratioIn(rect: DOMRect, y: number) {
    return rect.height ? Math.min(1, Math.max(0, (y - rect.top) / rect.height)) : 0
  }

  function sectionId(el: Element) {
    const id = el.getAttribute('data-drop-section')
    return id === UNCATEGORIZED_SECTION ? null : id
  }

  // The section the pointer is in, or else the last one above it.
  function sectionAt(y: number) {
    let found: { el: Element; rect: DOMRect; inside: boolean } | null = null
    for (const el of navEl.value?.querySelectorAll('[data-drop-section]') ?? []) {
      const rect = el.getBoundingClientRect()
      if (rect.top <= y) found = { el, rect, inside: y < rect.bottom }
    }
    return found
  }

  function channelHit(y: number): DropHit {
    const section = sectionAt(y)
    if (!section) return { zone: 'start' }
    if (!section.inside) return { zone: 'end', id: sectionId(section.el) }
    // The row (or header) under the pointer, else the nearest one in this section.
    let best: { el: Element; rect: DOMRect; distance: number } | null = null
    for (const el of section.el.querySelectorAll('[data-drop]')) {
      const rect = el.getBoundingClientRect()
      const distance = y < rect.top ? rect.top - y : y >= rect.bottom ? y - rect.bottom : 0
      if (!best || distance < best.distance) best = { el, rect, distance }
    }
    if (!best) return { zone: 'end', id: sectionId(section.el) }
    const zone = best.el.getAttribute('data-drop')
    const id = best.el.getAttribute('data-id')
    // The people under a voice channel belong to its lower half.
    if (zone === 'channel-tail' && id) return { zone: 'channel', id, ratio: 1 }
    if ((zone !== 'channel' && zone !== 'header' && zone !== 'empty') || !id) return { zone: 'end', id: sectionId(section.el) }
    return { zone, id, ratio: ratioIn(best.rect, y) }
  }

  function categoryHit(y: number): DropHit {
    const section = sectionAt(y)
    if (!section) return { zone: 'start' }
    return { zone: 'section', id: sectionId(section.el), ratio: section.inside ? ratioIn(section.rect, y) : 1 }
  }

  function resolveDrop({ x, y, item }: { x: number; y: number; item: SidebarItem }): SidebarTarget | null {
    const nav = navEl.value
    if (!nav) return null
    const r = nav.getBoundingClientRect()
    // Outside the channel list: dropping there cancels.
    if (x < r.left || x > r.right || y < r.top || y > r.bottom) return null
    if (item.kind === 'category') return resolveCategoryDrop(layout.value, item.id, categoryHit(y))
    const hit = channelHit(y)
    const target = resolveChannelDrop(layout.value, item.id, hit, { isCollapsed })
    // Only the header itself opens a collapsed category for a peek; the free
    // space after the last one appends to it without opening it.
    return target && { ...target, overHeader: hit.zone === 'header' }
  }

  function handleDrop(item: SidebarItem, target: SidebarTarget) {
    if (item.kind === 'category') {
      applyMove(item, moveCategory(layout.value, item.id, target.index))
      return
    }
    // A category opened by hovering stays open when the channel went into it.
    if (target.categoryId && peeked.value.has(target.categoryId)) expandCategory(target.categoryId)
    applyMove(item, moveChannel(layout.value, item.id, target.categoryId, target.index))
  }

  function handleLongPress(item: SidebarItem, e: PointerEvent) {
    if (item.kind === 'channel') {
      const at = locateChannel(layout.value, item.id)
      if (at) onLongPress?.(item.kind, at.channel, e)
    } else {
      const at = locateCategory(layout.value, item.id)
      if (at) onLongPress?.(item.kind, at.category, e)
    }
  }

  const drag = useSortableDrag<SidebarItem, SidebarTarget>({
    enabled,
    scrollContainer: navEl,
    // Targets are found by height (channelHit), not by the element under the pointer.
    hitTest: () => null,
    resolve: resolveDrop,
    onDrop: handleDrop,
    onEnd: () => closePeeks(),
    onLongPress: handleLongPress
  })

  const dragItem = computed(() => (drag.state.phase === 'dragging' ? drag.state.item : null))

  function startDrag(e: PointerEvent, kind: ItemKind, entity: Channel | Category) {
    if (enabled()) drag.pointerDown(e, itemOf(entity))
  }

  function isDragged(kind: ItemKind, id: string) {
    return dragItem.value?.kind === kind && dragItem.value.id === id
  }

  /** 'top' | 'bottom' | 'inside' | '' for an indicator key ('channel:<id>', 'header:<id>', 'section:<id>'). */
  function indicatorFor(key: string) {
    const target = dragItem.value && drag.state.target
    return target?.indicator?.key === key ? target.indicator.edge : ''
  }

  // Holding a channel over a collapsed category's header opens it after a moment.
  watch(() => drag.state.target, target => {
    const id = target?.indicator?.edge === 'inside' && target.overHeader ? target.categoryId : null
    if (id === peekFor) return
    clearTimeout(peekTimer)
    peekFor = id
    if (!id) return
    peekTimer = setTimeout(() => {
      peeked.value = new Set([...peeked.value, id])
      nextTick(() => drag.refresh())
    }, PEEK_MS)
  })

  function closePeeks() {
    clearTimeout(peekTimer)
    peekTimer = undefined
    peekFor = null
    if (peeked.value.size) peeked.value = new Set()
  }

  // Members never get the touch listener, so their scrolling stays passive.
  const navListeners = computed(() => (enabled() ? { touchmove: drag.touchMove } : {}))

  watch(enabled, on => { if (!on) drag.cancel() })

  if (getCurrentScope()) {
    onScopeDispose(() => {
      clearTimeout(flashTimer)
      clearTimeout(peekTimer)
    })
  }

  return {
    navEl, hintId, flashKey, announcement, isCollapsed, reveal, moveByKey,
    drag, dragItem, startDrag, isDragged, indicatorFor, navListeners
  }
}
