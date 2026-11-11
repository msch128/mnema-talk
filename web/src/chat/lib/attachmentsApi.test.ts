import { afterEach, describe, expect, it, vi } from 'vitest'
import { stageAttachment, unstageAttachment } from './attachmentsApi'

afterEach(() => vi.unstubAllGlobals())

const staged = { id: '11111111-1111-4111-8111-111111111111', original_filename: 'a.png', mime_type: 'image/png', size_bytes: 3, url: '/api/media/11111111-1111-4111-8111-111111111111', is_deleted: false }

describe('attachmentsApi', () => {
  it('uploads the file as multipart to the channel and returns the staged attachment', async () => {
    const fetchSpy = vi.fn<typeof fetch>(async () => new Response(JSON.stringify(staged), { status: 201 }))
    vi.stubGlobal('fetch', fetchSpy)
    const file = new File(['abc'], 'a.png', { type: 'image/png' })
    const got = await stageAttachment('ch1', file)
    expect(got.id).toBe('11111111-1111-4111-8111-111111111111')
    const [path, init] = fetchSpy.mock.calls[0] ?? []
    expect(path).toBe('/api/channels/ch1/attachments')
    expect(init?.method).toBe('POST')
    expect((init?.body as FormData).get('file')).toBeInstanceOf(File)
  })

  it('passes the abort signal on', async () => {
    const fetchSpy = vi.fn<typeof fetch>(async () => new Response(JSON.stringify(staged), { status: 201 }))
    vi.stubGlobal('fetch', fetchSpy)
    const ctl = new AbortController()
    await stageAttachment('ch1', new File(['x'], 'a.png'), { signal: ctl.signal })
    expect(fetchSpy.mock.calls[0]?.[1]?.signal).toBe(ctl.signal)
  })

  it('removes a staged file with DELETE', async () => {
    const fetchSpy = vi.fn<typeof fetch>(async () => new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchSpy)
    await unstageAttachment('ch1', 'a1')
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/channels/ch1/attachments/a1')
    expect(fetchSpy.mock.calls[0]?.[1]?.method).toBe('DELETE')
  })
})
