// The calls of the wiki (doc/wiki.md). Page addresses are encoded here, so a
// caller passes the slug as it is.
import { api } from '../shared/lib/api'
import {
  decodeWikiImage, decodeWikiPage, decodeWikiRevision, decodeWikiTreeResponse, isWikiRevision, isWikiSearchHit,
  type WikiImage, type WikiPage, type WikiPageChange, type WikiPageInput, type WikiPageSummary, type WikiRevision, type WikiRestoreInput, type WikiSearchHit
} from '../types/rest'
import { arrayDecoder } from '../types/validation'

export type { WikiImage, WikiPage, WikiPageChange, WikiPageInput, WikiPageSummary, WikiRevision, WikiSearchHit }

const decodeRevisions = arrayDecoder('wiki revisions', isWikiRevision)
const decodeHits = arrayDecoder('wiki search hits', isWikiSearchHit)

const pagePath = (slug: string) => `/api/wiki/pages/${encodeURIComponent(slug)}`

export async function fetchWikiTree(deleted: boolean): Promise<WikiPageSummary[]> {
  const tree = await api(deleted ? '/api/wiki?deleted=true' : '/api/wiki', { decode: decodeWikiTreeResponse })
  return tree.pages
}

export function fetchWikiPage(slug: string): Promise<WikiPage> {
  return api(pagePath(slug), { decode: decodeWikiPage })
}

export function createWikiPage(input: WikiPageInput): Promise<WikiPage> {
  return api('/api/wiki/pages', { method: 'POST', json: input, decode: decodeWikiPage })
}

export function changeWikiPage(slug: string, change: WikiPageChange): Promise<WikiPage> {
  return api(pagePath(slug), { method: 'PATCH', json: change, decode: decodeWikiPage })
}

export function deleteWikiPage(slug: string): Promise<WikiPage> {
  return api(pagePath(slug), { method: 'DELETE', decode: decodeWikiPage })
}

export function undeleteWikiPage(slug: string): Promise<WikiPage> {
  return api(`${pagePath(slug)}/undelete`, { method: 'POST', decode: decodeWikiPage })
}

export function lockWikiPage(slug: string, locked: boolean): Promise<WikiPage> {
  return api(`${pagePath(slug)}/lock`, { method: 'PUT', json: { locked }, decode: decodeWikiPage })
}

/** A page of revisions, newest first; `before` continues below that revision number. */
export function fetchWikiRevisions(slug: string, before?: number): Promise<WikiRevision[]> {
  const query = before ? `?before=${before}` : ''
  return api(`${pagePath(slug)}/revisions${query}`, { decode: decodeRevisions })
}

export function fetchWikiRevision(slug: string, rev: number): Promise<WikiRevision> {
  return api(`${pagePath(slug)}/revisions/${rev}`, { decode: decodeWikiRevision })
}

export function restoreWikiRevision(slug: string, rev: number, input: WikiRestoreInput): Promise<WikiPage> {
  return api(`${pagePath(slug)}/revisions/${rev}/restore`, { method: 'POST', json: input, decode: decodeWikiPage })
}

export function searchWiki(query: string): Promise<WikiSearchHit[]> {
  return api(`/api/wiki/search?q=${encodeURIComponent(query)}`, { decode: decodeHits })
}

/** Stores a picture for use in pages; the answer holds the markdown to put in the text. */
export function uploadWikiImage(file: File): Promise<WikiImage> {
  const form = new FormData()
  form.append('image', file)
  return api('/api/wiki/images', { method: 'POST', form, decode: decodeWikiImage })
}
