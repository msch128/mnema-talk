// The avatar of a member in an event: UserAvatar takes one `user` object, and
// the API sends a missing picture as null.
import type { AvatarUser } from '../components/presentationTypes'
import type { EventUserRef } from '../types/domain'

export function eventAvatar(user: EventUserRef): AvatarUser {
  return { display_name: user.display_name, username: user.username, avatar_url: user.avatar_url ?? undefined }
}
