import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import { decodeChatForwardQuote, type ChatForwardQuote } from '../types/rest'

// What the reader may see of the original of each forwarded message on screen
// (GET /api/messages/{id}/forward). The server decides per reader, so the
// quote is only ever fetched, never built from the forwarded message. Any
// edit or delete in the community may change one, so those events drop the
// cache and the quotes on screen ask again.
export const useForwardQuotesStore = defineStore('forwardQuotes', () => {
  const quotes = ref(new Map<string, ChatForwardQuote>())
  /** Changes whenever the cached quotes may be out of date. */
  const epoch = ref(0)
  const pending = new Map<string, Promise<void>>()

  /** The quote of a forwarded message, once loaded (null before, and when it cannot be read). */
  function quoteOf(forwardedId: string): ChatForwardQuote | null {
    return quotes.value.get(forwardedId) ?? null
  }

  /** Loads the quote unless it is cached or on its way. */
  function ensure(forwardedId: string): Promise<void> {
    if (quotes.value.has(forwardedId)) return Promise.resolve()
    const running = pending.get(forwardedId)
    if (running) return running
    const generation = epoch.value
    const request = api(`/api/messages/${forwardedId}/forward`, { decode: decodeChatForwardQuote }).then(
      quote => { if (generation === epoch.value) quotes.value.set(forwardedId, quote) },
      () => undefined
    ).finally(() => { if (pending.get(forwardedId) === request) pending.delete(forwardedId) })
    pending.set(forwardedId, request)
    return request
  }

  /** Some message changed or went away: forget what was read. */
  function invalidate() {
    quotes.value = new Map()
    pending.clear()
    epoch.value++
  }

  return { epoch, quoteOf, ensure, invalidate, reset: invalidate }
})
