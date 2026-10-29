// What keeps unsaved changes from being lost: the "Discard changes?" question, a
// scope that lets a container (dashboard, dialog with tabs) ask once for all the
// drafts inside it, and the browser's own warning when the page is closed.
import type { InjectionKey, Ref } from 'vue'
import { computed, inject, onBeforeUnmount, onMounted, provide, shallowReactive } from 'vue'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'

interface ScopeMember { dirty: Ref<boolean> }
interface DraftScope { members: Set<ScopeMember> }
const SCOPE: InjectionKey<DraftScope> = Symbol('settings-draft-scope')

/** Asks whether unsaved changes may be dropped. */
export function confirmDiscard(): Promise<boolean> {
  return confirm({
    title: t('draft.discardTitle'),
    body: t('draft.discardBody'),
    confirmLabel: t('draft.discard'),
    cancelLabel: t('draft.keepEditing'),
    danger: true
  })
}

/**
 * A container (the admin dashboard, the settings dialog) that holds several
 * drafts at once. It knows whether any of them is unsaved, so a tab switch or
 * the close button can ask once for all of them.
 */
export function provideDraftScope() {
  const scope: DraftScope = { members: shallowReactive(new Set<ScopeMember>()) }
  provide(SCOPE, scope)
  const dirty = computed(() => [...scope.members].some(m => m.dirty.value))
  return { dirty, guard: async () => !dirty.value || confirmDiscard() }
}


/** Registers a draft with the surrounding scope (if any) and warns the browser while it is unsaved. */
export function useLeaveGuard(dirty: Ref<boolean>) {
  const member: ScopeMember = { dirty }
  const scope = inject(SCOPE, null)
  scope?.members.add(member)
  const beforeUnload = (e: BeforeUnloadEvent) => { if (dirty.value) e.preventDefault() }
  onMounted(() => window.addEventListener('beforeunload', beforeUnload))
  onBeforeUnmount(() => {
    window.removeEventListener('beforeunload', beforeUnload)
    scope?.members.delete(member)
  })
}

/** Ctrl/Cmd+S runs `save` and keeps the browser's own "save page" away. */
export function onSaveShortcut(e: KeyboardEvent, save: () => unknown) {
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 's') {
    e.preventDefault()
    void save()
  }
}
