// The state and the saving of the wiki editor: the text being written, whether
// it differs from what was read, the size, and what happens when somebody saved
// meanwhile (the server refuses the save and nothing is overwritten). What is
// written is also kept in the tab's session storage, so a reload brings it back.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { changeWikiPage, createWikiPage, type WikiPage } from '../lib/wikiApi'
import { wikiDraftKey, type WikiDraft } from '../lib/wikiDraft'
import { goWiki, wikiDirty } from '../lib/wikiNav'
import { useAuthStore } from '../../stores/auth'
import { keepWikiDraft, resumableDraft } from './useWikiDraft'
import { useStaleSave } from './useStaleSave'
import { useWikiStore } from '../../stores/wiki'

/** How many editors are open: an unsaved flag left by a closed one is cleared only when none is. */
let editors = 0

export const MAX_BYTES = 50 * 1024

const encoder = new TextEncoder()

interface EditorFields { title: string; content: string; summary: string; slug: string; parent: string }

/** Creates the page (page null) or changes it on top of baseRev. */
function submitEditor(page: WikiPage | null, baseRev: number, f: EditorFields): Promise<WikiPage> {
  const summary = f.summary ? { summary: f.summary } : {}
  if (!page) {
    return createWikiPage({ title: f.title, content: f.content, ...(f.slug ? { slug: f.slug } : {}), ...(f.parent ? { parent_id: f.parent } : {}), ...summary })
  }
  return changeWikiPage(page.slug, { base_rev: baseRev, title: f.title, content: f.content, ...summary })
}

/** The text fields of the editor, from a draft that came back or from the page; `reset` goes back to the page. */
function editorFields(page: WikiPage | null, parentId: string | null, start: WikiDraft | null) {
  const fresh: WikiDraft = { title: page?.title ?? '', content: page?.content ?? '', summary: '', slug: '', parent: parentId ?? '', baseRev: page?.rev ?? 0 }
  const from = start ?? fresh
  const fields = {
    title: ref(from.title), content: ref(from.content), summary: ref(from.summary),
    slug: ref(from.slug), parent: ref(from.parent), baseRev: ref(from.baseRev)
  }
  function reset() {
    for (const key of Object.keys(fresh) as (keyof WikiDraft)[]) Object.assign(fields[key], { value: fresh[key] })
  }
  return { ...fields, reset }
}

export function useWikiEditor(page: WikiPage | null, parentId: string | null = null) {
  const wiki = useWikiStore()
  const draftKey = wikiDraftKey(useAuthStore().user?.id ?? '', page?.slug ?? null)
  const start = resumableDraft(draftKey, page)
  const { title, content, summary, slug, parent, baseRev, reset } = editorFields(page, parentId, start)
  const error = ref('')
  const busy = ref(false)
  const restored = ref(!!start) // the text of a draft from before a reload is back in the fields

  const bytes = computed(() => encoder.encode(content.value).length)
  const tooLong = computed(() => bytes.value > MAX_BYTES)
  const dirty = computed(() => title.value !== (page?.title ?? '') || content.value !== (page?.content ?? ''))
  const canSave = computed(() => !busy.value && !tooLong.value && title.value.trim() !== '' && (page ? dirty.value : true))

  watch(dirty, value => { wikiDirty.value = value }, { immediate: true })
  const draft = keepWikiDraft(draftKey, { title, content, summary, slug, parent, baseRev }, dirty, () => wikiDirty.value)
  // An editor that is gone (the member went elsewhere) must not pull them back when its save comes in.
  let alive = true
  editors++
  onBeforeUnmount(() => { alive = false; editors-- })

  function leave(target: WikiPage) {
    wikiDirty.value = false
    draft.drop()
    void goWiki({ mode: 'page', slug: target.slug }, true)
  }

  /** The save of an editor that was left meanwhile went through: its text is on the server, nothing is held any more. */
  function finishUnseen() {
    draft.drop()
    if (editors === 0) wikiDirty.value = false
  }

  /** Saves; true when it went through. A refused save keeps the text and opens the conflict view. */
  async function save(): Promise<boolean> {
    if (!canSave.value) return false
    busy.value = true
    error.value = ''
    try {
      const saved = await submitEditor(page, baseRev.value, { title: title.value, content: content.value, summary: summary.value.trim(), slug: slug.value.trim(), parent: parent.value })
      wiki.applyPage(saved, alive)
      await wiki.loadTree()
      if (alive) leave(saved)
      else finishUnseen()
      return true
    } catch (e) {
      await explain(e)
      return false
    } finally {
      busy.value = false
    }
  }

  const { theirs, explain, saveOnTop, takeTheirs } = useStaleSave({ page, baseRev, error, save: () => save(), leave })

  /** Drops the text of the draft that came back after a reload and starts again from the page. */
  function discardDraft() {
    draft.drop()
    reset()
    restored.value = false
  }

  return { title, content, summary, slug, parent, error, busy, theirs, restored, bytes, tooLong, dirty, canSave, save, saveOnTop, takeTheirs, discardDraft }
}
