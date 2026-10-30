// How the saved list reads: filtered by a search, then either newest saved
// first or grouped by where the message was written. Pure.
import type { ChatSavedMessage } from '../types/rest'
import { shownName } from './deletedUser'
import { previewText } from './replies'

export type SavedSort = 'recent' | 'channel'

/** Where a saved message lives: a channel, a voice channel's chat or a conversation. */
export interface SavedContext { name: string; dm: boolean; voice: boolean }

export type SavedContextOf = (channelId: string) => SavedContext | undefined

export interface SavedGroup {
  key: string
  /** Null for the one ungrouped list of the "recent" sort. */
  context: SavedContext | null
  gone: boolean
  entries: ChatSavedMessage[]
}

const fold = (text: string) => text.toLocaleLowerCase()

/** Whether the entry matches every word of the query in its text, note, author or channel. Entries that are gone only have their note. */
export function matchesSaved(entry: ChatSavedMessage, query: string, contextOf: SavedContextOf): boolean {
  const words = fold(query).split(/\s+/).filter(Boolean)
  if (!words.length) return true
  const message = entry.message
  const haystack = fold([
    entry.note,
    message ? previewText(message.content) : '',
    message ? shownName(message) : '',
    message ? message.username : '',
    message ? contextOf(message.channel_id)?.name ?? '' : ''
  ].join('\n'))
  return words.every(word => haystack.includes(word))
}

/** The entries in the order and grouping of the sort; entries that are gone sit together at the end of a grouped list. */
export function groupSaved(entries: readonly ChatSavedMessage[], sort: SavedSort, contextOf: SavedContextOf): SavedGroup[] {
  if (sort === 'recent') return entries.length ? [{ key: 'all', context: null, gone: false, entries: [...entries] }] : []
  const groups = new Map<string, SavedGroup>()
  const gone: SavedGroup = { key: 'gone', context: null, gone: true, entries: [] }
  for (const entry of entries) {
    if (!entry.available || !entry.message) {
      gone.entries.push(entry)
      continue
    }
    const id = entry.message.channel_id
    const known = groups.get(id)
    if (known) known.entries.push(entry)
    else groups.set(id, { key: id, context: contextOf(id) ?? { name: '', dm: false, voice: false }, gone: false, entries: [entry] })
  }
  return [...groups.values(), ...(gone.entries.length ? [gone] : [])]
}
