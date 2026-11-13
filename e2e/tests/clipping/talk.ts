import { randomUUID } from 'node:crypto'
import type { Page, WebSocketRoute } from '@playwright/test'
import { expect } from '../fixtures'
import { apiFetch, expectTalkLive } from '../helpers'

// A Talk for the sweep: the Owner joins for real (fake devices), and the other
// seats are filled by voice_state_update events the test hands to the page on
// its own socket. The stage, the sidebar and the member list lay those seats
// out exactly as they would real members; nobody's media is involved, which
// is all a layout check needs and what keeps 25 seats affordable.

export interface Socket { send(event: unknown): void }

/** Routes the page's realtime socket through the test (call before the page loads). */
export async function routeSocket(page: Page): Promise<Socket> {
  let route: WebSocketRoute | undefined
  await page.routeWebSocket(/\/api\/ws/, (ws) => {
    ws.connectToServer()
    route = ws
  })
  return { send: (event) => route?.send(JSON.stringify(event)) }
}

/** Headless browsers have no screen to pick: the shared "screen" is an animated canvas. */
export function fakeDisplayMedia() {
  MediaDevices.prototype.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = 1280
    canvas.height = 720
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas 2D context unavailable')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n++ * 7) % 360} 60% 40%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
    }, 100)
    return canvas.captureStream(10)
  }
}

export async function joinTalk(page: Page, talkName: string, takeoverLabel: string) {
  const drawer = page.getByTestId('channel-drawer')
  if (await drawer.evaluate(el => el.hasAttribute('inert'))) await page.getByTestId('open-nav').click()
  await page.locator('[data-channel-type="voice"]', { hasText: talkName }).first().click()
  const takeover = page.getByRole('button', { name: takeoverLabel })
  await takeover.or(page.getByTestId('voice-panel-leave')).first().waitFor()
  if (await takeover.isVisible()) await takeover.click()
  await expectTalkLive(page)
}

const NAMES = ['Alexandria Montgomery', 'Bo', 'Christopher Nightingale', 'Dee', 'Evangeline Worthington', 'Finn', 'Gwendolyn Fairweather']

/** Seats `count` invented members in the Talk the page is in (on top of the ones already there). */
export async function seatParticipants(page: Page, socket: Socket, channelId: string, count: number, from = 0) {
  const me = await apiFetch(page, 'GET', '/api/auth/me')
  const base = me.json as Record<string, unknown>
  const now = new Date().toISOString()
  for (let i = from; i < from + count; i++) {
    const name = `${NAMES[i % NAMES.length] ?? 'Member'} ${i}`.slice(0, 24)
    const user = { ...base, id: randomUUID(), username: `seat_${i}`, display_name: name, role: 'user', avatar_url: '', joined_at: now, muted: i % 3 === 1, deafened: i % 5 === 2 }
    socket.send({ type: 'voice_state_update', payload: { channel_id: channelId, action: 'join', user, started_at: now } })
  }
  await expect(page.locator('nav [data-voice-user]')).toHaveCount(from + count + 1, { timeout: 10_000 })
}
