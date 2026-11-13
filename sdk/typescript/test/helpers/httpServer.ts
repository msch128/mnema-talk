// A local HTTP server that records requests and answers from a handler.
import { createServer, type IncomingHttpHeaders, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'

export interface RecordedRequest {
  method: string
  url: string
  headers: IncomingHttpHeaders
  body: Buffer
  json(): any
}

export type Handler = (req: RecordedRequest, res: ServerResponse) => void

export interface TestHttpServer {
  url: string
  requests: RecordedRequest[]
  close(): Promise<void>
}

export function sendJson(res: ServerResponse, status: number, body: unknown, headers: Record<string, string> = {}): void {
  res.writeHead(status, { 'Content-Type': 'application/json', ...headers })
  res.end(body === undefined ? '' : JSON.stringify(body))
}

export async function startHttpServer(handler: Handler): Promise<TestHttpServer> {
  const requests: RecordedRequest[] = []
  const server = createServer((req, res) => {
    const chunks: Buffer[] = []
    req.on('data', (c: Buffer) => chunks.push(c))
    req.on('end', () => {
      const body = Buffer.concat(chunks)
      const rec: RecordedRequest = {
        method: req.method ?? '',
        url: req.url ?? '',
        headers: req.headers,
        body,
        json: () => JSON.parse(body.toString('utf8')),
      }
      requests.push(rec)
      handler(rec, res)
    })
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address() as AddressInfo
  return {
    url: `http://127.0.0.1:${port}`,
    requests,
    async close() {
      server.closeAllConnections()
      await new Promise<void>((resolve) => server.close(() => resolve()))
    },
  }
}
