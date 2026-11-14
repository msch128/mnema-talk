// What the realtime socket being down means for the events a call sends.
// sendSocketEvent drops an event while the socket is down, and that is right for
// most of them: a stale event replayed after a reconnect is a bug of its own.
//  - voice_join waits for the socket (socketUp), and a reconnect sends no
//    resume beside a join that is not sent yet.
//  - voice_leave during a drop is dropped: the server keeps the lost seat for
//    its grace time. A leave from the new connection would not end it (the
//    server ends a seat only for the connection that holds it), and a made-up
//    resume to take the seat first could evict the member's call on another
//    device. A server-side leave that names its Talk and tab is the proper fix.
//  - Stage hand and talk power are explicit clicks whose result comes from the
//    server's room state, and a reaction lives for seconds: dropping those
//    leaves nothing that disagrees, and the connection banner is showing.
//  - A screen share or camera started or stopped during the gap needs nothing:
//    the server drops the media connection of the lost socket with all its
//    tracks, and the resume negotiates a new one that announces what is sent.
import { watch } from 'vue'
import type { Cx } from './state'

/** Resolves once the realtime socket is connected. */
export function socketUp({ chat }: Cx): Promise<void> {
  return new Promise((resolve) => {
    const stop = watch(() => chat.isConnected, (up) => {
      if (!up) return
      stop()
      resolve()
    })
  })
}
