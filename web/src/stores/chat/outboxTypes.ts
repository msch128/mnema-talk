/** A text message written while the connection was down. It lives in memory only: a reload drops it. */
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
  /** waiting: not sent yet; sending: the request is out; failed: the server refused it (the reason is in `error`). */
  status: 'waiting' | 'sending' | 'failed'
  error: string
}
