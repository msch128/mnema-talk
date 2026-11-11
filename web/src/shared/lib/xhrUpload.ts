// A multipart request with upload progress. fetch cannot report how much of a
// body has been sent, so uploads that show a progress bar go through XHR.
// Same-origin cookies only, like the browser transport.

export interface XhrReply {
  status: number
  body: unknown
}

export function xhrUpload(
  path: string,
  method: string,
  form: FormData,
  onProgress: (loaded: number, total: number) => void,
  signal?: AbortSignal
): Promise<XhrReply> {
  return new Promise((resolve, reject) => {
    const abortError = () => Object.assign(new Error('aborted'), { name: 'AbortError' })
    if (signal?.aborted) {
      reject(abortError())
      return
    }
    const xhr = new XMLHttpRequest()
    const onAbort = () => xhr.abort()
    signal?.addEventListener('abort', onAbort, { once: true })
    const done = () => signal?.removeEventListener('abort', onAbort)
    xhr.open(method, path)
    xhr.withCredentials = false
    xhr.responseType = 'text'
    xhr.upload.onprogress = e => {
      if (e.lengthComputable) onProgress(e.loaded, e.total)
    }
    xhr.onload = () => {
      done()
      let body: unknown = null
      if (xhr.status !== 204) {
        try { body = JSON.parse(xhr.responseText) } catch { body = null }
      }
      resolve({ status: xhr.status, body })
    }
    xhr.onerror = () => { done(); reject(new Error('network')) }
    xhr.onabort = () => { done(); reject(abortError()) }
    xhr.send(form)
  })
}
