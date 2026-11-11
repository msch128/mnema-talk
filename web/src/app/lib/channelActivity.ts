// What a group of channels has waiting: the unread dot and mention count a
// collapsed category shows, and the count in the tab title.

export interface ActivityState { unread_count?: number; mention_count?: number; notify_level?: string }

export interface Activity {
  /** Channels with unread messages that may notify (muted ones do not count). */
  unreadChannels: number
  /** Mentions over all channels; a mention counts even in a muted channel. */
  mentions: number
}

export function aggregateActivity(
  channels: ReadonlyArray<{ id: string }>,
  states: Readonly<Record<string, ActivityState | undefined>>
): Activity {
  let unreadChannels = 0
  let mentions = 0
  for (const ch of channels) {
    const state = states[ch.id]
    if (!state) continue
    mentions += state.mention_count || 0
    if (state.notify_level !== 'mute' && (state.unread_count || 0) > 0) unreadChannels++
  }
  return { unreadChannels, mentions }
}

/** The prefix for the tab title: a mention count, a dot, or nothing. */
export function titlePrefix(activity: Activity): string {
  if (activity.mentions > 0) return `(${activity.mentions > 99 ? '99+' : activity.mentions}) `
  return activity.unreadChannels > 0 ? '• ' : ''
}
