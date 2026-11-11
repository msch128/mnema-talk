// Shared builders for the wiki tests.
import { usePermissionsStore } from './stores/permissions'
import type { WikiPage, WikiPageSummary, WikiRevision } from './wiki/lib/wikiApi'
import type { PermissionsInfo } from './types/domain'

export const VIEW_WIKI = 1 << 29
export const EDIT_WIKI = 1 << 27
export const MANAGE_WIKI = 1 << 28

export function grantWiki(bits: number): void {
  const info: PermissionsInfo = { permissions: bits, catalog: [], channels: [], role_ids: [], max_upload_mb: 50, owner: false, top_position: 0 }
  usePermissionsStore().info = info
}

export function summary(id: string, title: string, extra: Partial<WikiPageSummary> = {}): WikiPageSummary {
  return { id, slug: title.toLowerCase().replace(/ /g, '-'), title, parent_id: null, position: 0, locked: false, updated_at: '2026-03-01T10:00:00Z', ...extra }
}

export function wikiPage(extra: Partial<WikiPage> = {}): WikiPage {
  return {
    id: 'p1', slug: 'faq', title: 'FAQ', content: 'Hello **world**', rev: 3, locked: false, parent_id: null, position: 0,
    created_at: '2026-01-01T10:00:00Z', updated_at: '2026-03-01T10:00:00Z',
    updated_by: { id: 'u1', username: 'eda', display_name: 'Eda' },
    ...extra
  }
}

export function revision(rev: number, extra: Partial<WikiRevision> = {}): WikiRevision {
  return { rev, summary: '', size: 100, created_at: `2026-03-0${Math.min(rev, 9)}T10:00:00Z`, title: 'FAQ', content: `text ${rev}`, editor: { id: 'u1', username: 'eda', display_name: 'Eda' }, ...extra }
}
