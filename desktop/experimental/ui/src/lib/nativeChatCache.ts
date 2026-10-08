import { decodeNativeChatDisplay, type NativeChatDisplay } from './nativeChat'
export interface NativeChatCache { snapshot(): NativeChatDisplay[]; store(rows: readonly NativeChatDisplay[]): void }
type Entry = { channel: string; rows: NativeChatDisplay[]; bytes: number }
const MAX_CHANNELS = 16
const MAX_ROWS = 200
// Counts UTF-8 JSON bytes of retained closed DTOs. Map/object overhead is
// separately bounded by the 16 channels and 200 rows per channel limits.
const MAX_BYTES = 2 * 1024 * 1024
const size = (row: NativeChatDisplay) => new TextEncoder().encode(JSON.stringify(row)).length
const clone = (row: NativeChatDisplay): NativeChatDisplay => ({ ...row })
/** Session-only public projection cache; never authorizes or decrypts anything. */
export class NativeChatDisplayCache {
  private readonly entries = new Map<string, Entry>()
  clear() { this.entries.clear() }
  scope(key: string, channel: string): NativeChatCache {
    return {
      snapshot: () => {
        const entry = this.entries.get(key)
        if (!entry) return []
        this.entries.delete(key); this.entries.set(key, entry)
        return entry.rows.map(clone)
      },
      store: rows => {
        const decoded = rows.map(decodeNativeChatDisplay)
        if (decoded.some(row => row.channel_id !== channel)) throw new Error('Wrong native cache channel')
        const previous = this.entries.get(key)
        if (previous && previous.channel !== channel) throw new Error('Wrong native cache scope')
        const merged = new Map((previous?.rows ?? []).map(row => [row.id, row]))
        for (const row of decoded) {
          const old = merged.get(row.id)
          if (old && JSON.stringify(old) !== JSON.stringify(row)) throw new Error('Conflicting native display identity')
          merged.set(row.id, clone(row))
        }
        const retained = [...merged.values()].sort((a, b) => a.number - b.number).slice(-MAX_ROWS)
        const entry: Entry = { channel, rows: retained, bytes: retained.reduce((sum, row) => sum + size(row), 0) }
        while (entry.bytes > MAX_BYTES && entry.rows.length) {
          const removed = entry.rows.shift()
          if (removed) entry.bytes -= size(removed)
        }
        this.entries.delete(key); this.entries.set(key, entry)
        while (this.entries.size > MAX_CHANNELS || [...this.entries.values()].reduce((sum, item) => sum + item.bytes, 0) > MAX_BYTES) {
          const oldest = this.entries.keys().next().value
          if (oldest === undefined) break
          this.entries.delete(oldest)
        }
      }
    }
  }
}
