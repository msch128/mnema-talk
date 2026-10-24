import { useToastStore } from '../../stores/toast'
import { usePermissionsStore } from '../../stores/permissions'
import { t } from '../../i18n'
import { navigate } from '../../lib/router'
import type { ServerEventPayloads } from '../../types/events'
import type { Cx } from './state'

// Create-a-Talk lobbies. Joining a lobby seats nobody: the server opens a
// temporary Talk and answers talk_moved; this module then joins that Talk like
// any other (see doc/talks.md).

/** How long a lobby may take to open a Talk. */
const LOBBY_WAIT_MS = 10_000

/** The lobby join that waits for its Talk. */
export interface PendingLobby {
  lobby: string
  resolve: (talk: string | null) => void
  timer: ReturnType<typeof setTimeout>
}

export function isLobby(cx: Cx, channelId: string): boolean {
  return cx.chat.allChannels.some(c => c.id === channelId && c.is_lobby === true)
}

function settle(cx: Cx, talk: string | null) {
  const pending = cx.st.pendingLobby
  if (!pending) return
  clearTimeout(pending.timer)
  cx.st.pendingLobby = null
  pending.resolve(talk)
}

/** Asks the server to open a Talk from the lobby; resolves to its id, or null when none opened. */
function openFromLobby(cx: Cx, lobbyId: string): Promise<string | null> {
  settle(cx, null)
  return new Promise(resolve => {
    const timer = setTimeout(() => {
      useToastStore().error(t('talk.lobbyUnavailable'))
      settle(cx, null)
    }, LOBBY_WAIT_MS)
    cx.st.pendingLobby = { lobby: lobbyId, resolve, timer }
    cx.chat.sendWSEvent('voice_join', { channel_id: lobbyId })
  })
}

/** talk_moved: the lobby this connection waits on opened a Talk. */
export function lobbyMoved(cx: Cx, p: ServerEventPayloads['talk_moved']) {
  if (cx.st.pendingLobby?.lobby === p.from_channel_id) settle(cx, p.channel_id)
}

/** voice_kicked for the lobby that is being waited on; true when it was one. */
export function lobbyRefused(cx: Cx, p: ServerEventPayloads['voice_kicked']): boolean {
  if (cx.st.pendingLobby?.lobby !== p.channel_id) return false
  const reason = p.reason
  useToastStore().error(t(
    reason === 'talk_rate_limited' ? 'talk.lobbyRateLimited'
      : reason === 'missing_permission' ? 'voice.noPermission'
        : reason === 'rules_not_accepted' ? 'voice.kicked'
          : 'talk.lobbyUnavailable'
  ))
  settle(cx, null)
  return true
}

/** Opens a Talk from the lobby and joins it. join is the ordinary join of a Talk. */
export async function joinViaLobby(cx: Cx, lobbyId: string, join: (talkId: string) => Promise<void>) {
  const talk = await openFromLobby(cx, lobbyId)
  if (!talk) return
  // The new Talk is not in the channel list or the permissions yet; the join
  // needs both (it checks Connect for the Talk).
  await Promise.all([cx.chat.fetchChannels(), usePermissionsStore().load()])
  const channel = cx.chat.allChannels.find(c => c.id === talk)
  if (channel) cx.chat.selectChannel(channel)
  cx.voice.activeView = 'voice'
  navigate(`/v/${talk}`)
  await join(talk)
}
