// A scripted bot gateway on the test WebSocket server: HELLO, READY on
// IDENTIFY, RESUMED on RESUME, ACKs to heartbeats. Tests change the script.
import type { RequestListener } from 'node:http'
import { startWsServer, type TestSocket, type TestWsServer } from './wsServer.ts'

export interface FakeGatewayScript {
  heartbeatInterval: number
  /** Answer heartbeats with op 11 (default true). */
  ack: boolean
  /** Answer RESUME with RESUMED (true) or INVALID_SESSION (false). */
  resumable: boolean
  /** Runs instead of the default answer to IDENTIFY. */
  onIdentify?: (s: TestSocket, d: any) => void
}

export interface FakeGateway {
  server: TestWsServer
  url: string
  httpUrl: string
  script: FakeGatewayScript
  /** Sends a dispatch with the next sequence number. */
  dispatch(s: TestSocket, t: string, d: unknown): number
  ready(s: TestSocket, sessionId?: string): void
  close(): Promise<void>
}

export async function startFakeGateway(http?: RequestListener, script: Partial<FakeGatewayScript> = {}): Promise<FakeGateway> {
  let seq = 0
  let sessions = 0
  const s: FakeGatewayScript = { heartbeatInterval: 41250, ack: true, resumable: true, ...script }
  const fake = {} as FakeGateway
  const server = await startWsServer((sock) => {
    sock.send({ op: 10, d: { heartbeat_interval: s.heartbeatInterval } })
    sock.onFrame = (f) => {
      if (f.op === 1 && s.ack) sock.send({ op: 11 })
      if (f.op === 2) (s.onIdentify ?? ((so) => fake.ready(so)))(sock, f.d)
      if (f.op === 6) {
        if (s.resumable) sock.send({ op: 0, t: 'RESUMED', s: null, d: {} })
        else sock.send({ op: 9, d: false })
      }
    }
  }, http)
  Object.assign(fake, {
    server,
    url: server.url,
    httpUrl: server.httpUrl,
    script: s,
    dispatch(sock: TestSocket, t: string, d: unknown) {
      seq++
      sock.send({ op: 0, t, s: seq, d })
      return seq
    },
    ready(sock: TestSocket, sessionId = `session-${++sessions}`) {
      fake.dispatch(sock, 'READY', {
        v: 1,
        user: { id: 'user-1', bot_id: 'bot-1', username: 'helper', bot: true },
        session_id: sessionId,
        intents: 512,
        resume_gateway_url: server.url,
        heartbeat_interval: s.heartbeatInterval,
      })
    },
    close: () => server.close(),
  })
  return fake
}
