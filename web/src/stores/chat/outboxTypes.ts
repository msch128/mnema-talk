/** A text message written while the connection was down. It is kept in this browser (see outboxStorage.ts) until it is sent or discarded. */
export interface PendingMessage {
  /** The client's id for this send; the server keeps one message per author and nonce. */
  nonce: string
  channelId: string
  /** The account that wrote it: another account on the same page never sends it. */
  userId: string
  parentId: string | null
  replyToId: string | null
  content: string
  createdAt: string
  /** waiting: not sent yet; sending: the request is out; failed: the server refused it, or kept failing (the reason is in `error`). */
  status: 'waiting' | 'sending' | 'failed'
  error: string
  /** Why it waits: no connection, or the server answered "later" / failed. */
  cause: 'offline' | 'server'
  /** Failed tries of the server-side kind (5xx, unreadable answer); too many make it failed. */
  attempts: number
  /** A request with this text may have reached the server, so a changed text could not replace it: it can only be discarded. */
  attempted: boolean
  /** Not before this time (ms since epoch): the server asked to wait (slow mode). 0: no wait. */
  notBefore: number
}
