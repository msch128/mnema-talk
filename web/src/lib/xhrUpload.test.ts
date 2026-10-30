import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { xhrUpload } from './xhrUpload'

/** An XHR that lets the test play the server. */
class FakeXhr {
  static last: FakeXhr
  upload: { onprogress: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null } = { onprogress: null }
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  status = 0
  responseText = ''
  responseType = ''
  withCredentials = true
  method = ''
  url = ''
  sent: unknown
  constructor() { FakeXhr.last = this }
  open(method: string, url: string) { this.method = method; this.url = url }
  send(body: unknown) { this.sent = body }
  abort() { this.onabort?.() }
  reply(status: number, body: unknown) { this.status = status; this.responseText = body === null ? '' : JSON.stringify(body); this.onload?.() }
}

afterEach(() => vi.unstubAllGlobals())

describe('xhrUpload', () => {
  it('reports progress and resolves with the status and JSON body', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXhr)
    const seen: number[][] = []
    const form = new FormData()
    const done = xhrUpload('/api/x', 'POST', form, (loaded, total) => seen.push([loaded, total]))
    FakeXhr.last.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 10 })
    FakeXhr.last.upload.onprogress?.({ lengthComputable: false, loaded: 7, total: 0 })
    FakeXhr.last.reply(201, { id: 'a' })
    expect(await done).toEqual({ status: 201, body: { id: 'a' } })
    expect(seen).toEqual([[5, 10]])
    expect(FakeXhr.last.sent).toBe(form)
    expect(FakeXhr.last.method).toBe('POST')
  })

  it('treats an unreadable body as null', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXhr)
    const done = xhrUpload('/api/x', 'POST', new FormData(), () => undefined)
    FakeXhr.last.status = 502
    FakeXhr.last.responseText = '<html>'
    FakeXhr.last.onload?.()
    expect(await done).toEqual({ status: 502, body: null })
  })

  it('aborts with the signal', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXhr)
    const ctl = new AbortController()
    const done = xhrUpload('/api/x', 'POST', new FormData(), () => undefined, ctl.signal)
    ctl.abort()
    await expect(done).rejects.toMatchObject({ name: 'AbortError' })
    await expect(xhrUpload('/api/x', 'POST', new FormData(), () => undefined, ctl.signal)).rejects.toMatchObject({ name: 'AbortError' })
  })

  it('is what a form with a progress callback goes through, as an ApiError on failure', async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXhr)
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    const progress = vi.fn()
    const call = api('/api/channels/c/attachments', { method: 'POST', form: new FormData(), onUploadProgress: progress })
    FakeXhr.last.upload.onprogress?.({ lengthComputable: true, loaded: 1, total: 2 })
    FakeXhr.last.reply(413, { error: { code: 'PAYLOAD_TOO_LARGE', message: 'file exceeds the 1 MB limit' } })
    await expect(call).rejects.toMatchObject({ status: 413, code: 'PAYLOAD_TOO_LARGE' })
    expect(progress).toHaveBeenCalledWith(1, 2)
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('uses fetch for a form without a progress callback', async () => {
    const fetchSpy = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }))
    vi.stubGlobal('fetch', fetchSpy)
    await api('/api/x', { method: 'POST', form: new FormData() })
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})
