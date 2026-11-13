// A minimal WebSocket server for tests (RFC 6455 over node:http's upgrade):
// text frames, close frames with a code, ping. Enough to play the gateway.
import { createHash } from 'node:crypto'
import { createServer, type IncomingMessage, type RequestListener, type Server } from 'node:http'
import type { AddressInfo, Socket } from 'node:net'

const GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11'

export class TestSocket {
  readonly request: IncomingMessage
  readonly received: unknown[] = []
  closed = false
  closeCode: number | undefined
  private readonly socket: Socket
  private buffer = Buffer.alloc(0)
  private waiters: { pred: (f: unknown) => boolean; resolve: (f: unknown) => void }[] = []
  private closeWaiters: (() => void)[] = []
  onFrame: ((frame: any) => void) | undefined

  constructor(socket: Socket, request: IncomingMessage) {
    this.socket = socket
    this.request = request
    socket.on('data', (chunk: Buffer) => this.onData(chunk))
    socket.on('close', () => this.markClosed())
    socket.on('error', () => this.markClosed())
  }

  send(obj: unknown): void {
    this.writeFrame(0x1, Buffer.from(JSON.stringify(obj)))
  }

  /** Sends a close frame with a code and ends the TCP connection. */
  close(code = 1000, reason = ''): void {
    if (this.closed) return
    const payload = Buffer.alloc(2 + Buffer.byteLength(reason))
    payload.writeUInt16BE(code, 0)
    payload.write(reason, 2)
    this.writeFrame(0x8, payload)
    this.socket.end()
    this.markClosed()
  }

  /** Drops the TCP connection without a close frame (1006 for the client). */
  destroy(): void {
    this.socket.destroy()
    this.markClosed()
  }

  /** Resolves with the first received frame (past or future) that matches. */
  waitFor(pred: (f: any) => boolean): Promise<any> {
    const seen = this.received.find(pred)
    if (seen !== undefined) return Promise.resolve(seen)
    return new Promise((resolve) => this.waiters.push({ pred, resolve }))
  }

  waitForOp(op: number): Promise<any> {
    return this.waitFor((f: any) => f.op === op)
  }

  waitClosed(): Promise<void> {
    if (this.closed) return Promise.resolve()
    return new Promise((resolve) => this.closeWaiters.push(resolve))
  }

  private markClosed(): void {
    if (this.closed) return
    this.closed = true
    for (const w of this.closeWaiters.splice(0)) w()
  }

  private writeFrame(opcode: number, payload: Buffer): void {
    if (this.socket.destroyed) return
    const n = payload.length
    let header: Buffer
    if (n < 126) header = Buffer.from([0x80 | opcode, n])
    else if (n < 65536) {
      header = Buffer.alloc(4)
      header[0] = 0x80 | opcode
      header[1] = 126
      header.writeUInt16BE(n, 2)
    } else {
      header = Buffer.alloc(10)
      header[0] = 0x80 | opcode
      header[1] = 127
      header.writeBigUInt64BE(BigInt(n), 2)
    }
    this.socket.write(Buffer.concat([header, payload]))
  }

  private onData(chunk: Buffer): void {
    this.buffer = Buffer.concat([this.buffer, chunk])
    for (;;) {
      const frame = this.readFrame()
      if (!frame) return
      this.onWireFrame(frame.opcode, frame.payload)
    }
  }

  private readFrame(): { opcode: number; payload: Buffer } | undefined {
    const b = this.buffer
    if (b.length < 2) return undefined
    const opcode = b[0]! & 0x0f
    let len = b[1]! & 0x7f
    let offset = 2
    if (len === 126) {
      if (b.length < 4) return undefined
      len = b.readUInt16BE(2)
      offset = 4
    } else if (len === 127) {
      if (b.length < 10) return undefined
      len = Number(b.readBigUInt64BE(2))
      offset = 10
    }
    const masked = (b[1]! & 0x80) !== 0
    const maskEnd = offset + (masked ? 4 : 0)
    if (b.length < maskEnd + len) return undefined
    const payload = Buffer.from(b.subarray(maskEnd, maskEnd + len))
    if (masked) for (let i = 0; i < len; i++) payload[i]! ^= b[offset + (i % 4)]!
    this.buffer = b.subarray(maskEnd + len)
    return { opcode, payload }
  }

  private onWireFrame(opcode: number, payload: Buffer): void {
    if (opcode === 0x8) {
      this.closeCode = payload.length >= 2 ? payload.readUInt16BE(0) : 1005
      this.close(this.closeCode)
      return
    }
    if (opcode === 0x9) return this.writeFrame(0xa, payload)
    if (opcode !== 0x1) return
    const frame = JSON.parse(payload.toString('utf8'))
    this.received.push(frame)
    this.onFrame?.(frame)
    this.waiters = this.waiters.filter((w) => {
      if (!w.pred(frame)) return true
      w.resolve(frame)
      return false
    })
  }
}

export interface TestWsServer {
  /** The gateway address, ws://127.0.0.1:port/api/bot/v1/gateway. */
  url: string
  /** The server's HTTP address, http://127.0.0.1:port. */
  httpUrl: string
  /** Connections in the order they arrived. */
  sockets: TestSocket[]
  nextSocket(): Promise<TestSocket>
  close(): Promise<void>
}

/**
 * Starts a WebSocket server on 127.0.0.1; onConnection runs for every upgrade,
 * http answers plain requests (404 by default).
 */
export async function startWsServer(
  onConnection: (s: TestSocket) => void = () => {},
  http: RequestListener = (_req, res) => res.writeHead(404).end(),
): Promise<TestWsServer> {
  const sockets: TestSocket[] = []
  let waiters: ((s: TestSocket) => void)[] = []
  const server: Server = createServer(http)
  server.on('upgrade', (req, socket: Socket) => {
    const accept = createHash('sha1').update(String(req.headers['sec-websocket-key']) + GUID).digest('base64')
    socket.write(
      'HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n' +
        `Sec-WebSocket-Accept: ${accept}\r\n\r\n`,
    )
    const s = new TestSocket(socket, req)
    sockets.push(s)
    onConnection(s)
    for (const w of waiters.splice(0)) w(s)
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address() as AddressInfo
  let seen = 0
  return {
    url: `ws://127.0.0.1:${port}/api/bot/v1/gateway`,
    httpUrl: `http://127.0.0.1:${port}`,
    sockets,
    nextSocket() {
      if (sockets.length > seen) return Promise.resolve(sockets[seen++]!)
      return new Promise((resolve) => {
        waiters.push((s) => {
          seen++
          resolve(s)
        })
      })
    },
    async close() {
      for (const s of sockets) s.destroy()
      waiters = []
      server.closeAllConnections()
      await new Promise<void>((resolve) => server.close(() => resolve()))
    },
  }
}
