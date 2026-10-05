import { computed } from 'vue'
import { useVoiceStore } from '../stores/voice'

/**
 * Cards for every screen share that is not on the stage: my own while I share,
 * shares I watch, and shares I have not opted into yet.
 * state: 'idle' (not watched yet), 'pending' (watched, no media yet) or
 * 'queued' (can go on the stage).
 */
export function screenCardsFor({ users = [], myId = null, stage = null, ownStream = null, mediaState = {}, watched = {}, streams = {} }) {
  const cards = []
  if (ownStream && stage?.kind !== 'own') cards.push({ key: 'own', kind: 'own', user: null, state: 'queued' })
  for (const user of users) {
    if (user.id === myId || !mediaState[user.id]?.screen) continue
    if (stage?.kind === 'remote' && stage.userId === user.id) continue
    const state = !watched[user.id] ? 'idle' : streams[user.id] ? 'queued' : 'pending'
    cards.push({ key: user.id, kind: 'remote', user, state })
  }
  return cards
}

/**
 * The stage of a Talk: which share it shows and the cards to switch it.
 * `users`, `myId` and `active` are getters: the shown Talk's participants, my
 * user ID and whether I am connected to that Talk (a preview has neither stage
 * nor cards). `watch` opts into a share (it may have to join first).
 */
export function useTalkStage({ users, myId, active, watch }) {
  const voice = useVoiceStore()

  const stage = computed(() => (active() ? voice.stage : null))
  const ownOnStage = computed(() => stage.value?.kind === 'own')

  const cards = computed(() => {
    if (!active()) return []
    return screenCardsFor({
      users: users() || [],
      myId: myId(),
      stage: stage.value,
      ownStream: voice.localScreenStream,
      mediaState: voice.mediaState,
      watched: voice.watchedScreens,
      streams: voice.remoteScreenStreams
    })
  })

  function selectCard(card) {
    if (card.kind === 'own') voice.focusOwnScreen()
    else if (card.state === 'idle') (watch || voice.watchScreen)(card.user.id)
    else if (card.state === 'queued') voice.focusScreen(card.user.id)
  }

  return { stage, ownOnStage, cards, selectCard }
}
