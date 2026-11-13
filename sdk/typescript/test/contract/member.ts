// The signed-in member of the contract tests: the member REST routes (with
// the Origin header the CSRF check wants) and the member WebSocket.

export interface ContractEnv {
  url: string
  token: string
  channel: string
  cookie: string
}

/** The environment, or a skip reason when MNEMA_CONTRACT_URL is not set. */
export function contractEnv(): ContractEnv | string {
  const url = process.env.MNEMA_CONTRACT_URL
  if (!url) return 'MNEMA_CONTRACT_URL is not set: the contract tests need a running server (see README, Contract tests)'
  const need = ['MNEMA_CONTRACT_TOKEN', 'MNEMA_CONTRACT_CHANNEL', 'MNEMA_CONTRACT_MEMBER_COOKIE'] as const
  const missing = need.filter((k) => !process.env[k])
  if (missing.length > 0) throw new Error(`MNEMA_CONTRACT_URL is set but ${missing.join(', ')} not`)
  return {
    url: url.replace(/\/+$/, ''),
    token: process.env.MNEMA_CONTRACT_TOKEN!,
    channel: process.env.MNEMA_CONTRACT_CHANNEL!,
    cookie: process.env.MNEMA_CONTRACT_MEMBER_COOKIE!,
  }
}

export class Member {
  private readonly env: ContractEnv
  private ws: WebSocket | undefined
  private readonly events: { type: string; payload: any }[] = []
  private waiters: { type: string; pred: (p: any) => boolean; resolve: (p: any) => void }[] = []

  constructor(env: ContractEnv) {
    this.env = env
  }

  /** Calls a member route; fails unless the status is the expected one. */
  async call(method: string, path: string, body: unknown, expect: number): Promise<any> {
    const res = await fetch(this.env.url + path, {
      method,
      headers: { Cookie: this.env.cookie, Origin: this.env.url, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const text = await res.text()
    if (res.status !== expect) throw new Error(`member ${method} ${path}: expected ${expect}, got ${res.status}: ${text}`)
    return text ? JSON.parse(text) : undefined
  }

  /** The member's view of one message, through the message list around it. */
  async message(id: string): Promise<any> {
    const page = (await this.call('GET', `/api/channels/${this.env.channel}/messages?around=${id}&limit=5`, undefined, 200)) as any[]
    const m = page.find((x) => x.id === id)
    if (!m) throw new Error(`member cannot see message ${id}`)
    return m
  }

  /** Opens the member WebSocket (/api/ws) and keeps its events. */
  async listen(): Promise<void> {
    const wsUrl = this.env.url.replace(/^http/, 'ws') + '/api/ws'
    const Ctor = WebSocket as unknown as new (u: string, o: { headers: Record<string, string> }) => WebSocket
    const ws = new Ctor(wsUrl, { headers: { Cookie: this.env.cookie, Origin: this.env.url } })
    this.ws = ws
    ws.addEventListener('message', (ev) => this.onEvent(String(ev.data)))
    await new Promise<void>((resolve, reject) => {
      ws.addEventListener('open', () => resolve(), { once: true })
      ws.addEventListener('close', (e) => reject(new Error(`member WebSocket closed with ${e.code}`)), { once: true })
    })
  }

  /** The next (or an already received) event of this type that matches. */
  event(type: string, pred: (p: any) => boolean = () => true, timeoutMs = 10_000): Promise<any> {
    const seen = this.events.find((e) => e.type === type && pred(e.payload))
    if (seen) return Promise.resolve(seen.payload)
    return withTimeout(new Promise((resolve) => this.waiters.push({ type, pred, resolve })), timeoutMs, `member event ${type}`)
  }

  close(): void {
    this.ws?.close()
  }

  private onEvent(text: string): void {
    for (const line of text.split('\n')) {
      if (!line.trim()) continue
      const e = JSON.parse(line) as { type: string; payload: any }
      this.events.push(e)
      this.waiters = this.waiters.filter((w) => {
        if (w.type !== e.type || !w.pred(e.payload)) return true
        w.resolve(e.payload)
        return false
      })
    }
  }
}

export function withTimeout<T>(p: Promise<T>, ms: number, what: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`timed out after ${ms} ms waiting for ${what}`)), ms)
  })
  return Promise.race([p, timeout]).finally(() => clearTimeout(timer))
}
