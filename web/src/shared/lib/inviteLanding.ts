// Where someone who signed up with an invite from a channel's menu lands: in that channel, when the
// channel list the server then sends them holds it; in the default place otherwise. The invite preview
// only names a channel every new member can see, and this checks again against what they were sent.
import { channelPath, type PathChannel } from './routePaths'

let landing = ''

/** Remembers the channel of the invite just used (nothing for an invite without one). */
export function rememberInviteLanding(channelId: string | undefined) {
  landing = channelId ?? ''
}

/** The address of the remembered channel if the member can see it, else null; forgets it either way. */
export function inviteLandingRoute(channels: readonly (PathChannel & { id: string })[]): string | null {
  const wanted = landing
  landing = ''
  const channel = wanted ? channels.find(c => c.id === wanted) : undefined
  return channel ? channelPath(channel) : null
}
