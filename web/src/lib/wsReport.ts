// When an admin opens Status, the page tells the server whether its own live connection (/api/ws)
// came through the reverse proxy as an upgraded WebSocket; the server shows that as the WebSocket item.
import { api } from './api'

const sleep = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms))

/** Reports the admin's own connection. Reports only an open socket; one still connecting gets a moment first. */
export async function reportWebSocket(connected: () => boolean, settleMs = 3000): Promise<void> {
  for (let waited = 0; !connected() && waited < settleMs; waited += 250) await sleep(250)
  // Only an open socket is worth reporting: one that is not open yet says nothing about the proxy.
  if (!connected()) return
  try {
    await api('/api/admin/status/websocket', { method: 'POST', json: { connected: true } })
  } catch {
    // Advisory: a failed report only leaves the item as it was.
  }
}
