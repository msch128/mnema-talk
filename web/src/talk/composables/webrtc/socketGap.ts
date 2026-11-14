// What the realtime socket being down means for the events a call sends.
// sendSocketEvent drops an event while the socket is down, and that is right for
// most of them: a stale event replayed after a reconnect is a bug of its own.
//  - voice_join waits for the socket (socketUp), and a reconnect sends no
//    resume beside a join that is not sent yet.
//  - voice_leave: whenever a join was sent, leave sends voice_leave {channel_id, tab}
//    also when the socket is connected (a seated connection leaves; a reconnected-
//    but-not-yet-resumed one ends the lost seat). While the socket is down it
//    cannot go out immediately: the call remembers the channel id in leaveUnsent
//    and sends one voice_leave {channel_id, tab} after the reconnect to end the
//    lost seat without sending a made-up voice_join. A later join clears the
//    owed leave. When no join was sent, leave sends {} if connected.
//  - Stage hand and talk power are explicit clicks whose result comes from the
//    server's room state, and a reaction lives for seconds: dropping those
//    leaves nothing that disagrees, and the connection banner is showing.
//  - A screen share or camera started or stopped during the gap needs nothing:
//    the server drops the media connection of the lost socket with all its
//    tracks, and the resume negotiates a new one that announces what is sent.
import { watch } from 'vue'
import type { Cx } from './state'

/** Resolves once the realtime socket is connected; callers ask only while it is down, so the next change is the connect. */
export function socketUp({ chat }: Cx): Promise<void> {
  return new Promise((resolve) => {
    watch(() => chat.isConnected, () => resolve(), { once: true })
  })
}
