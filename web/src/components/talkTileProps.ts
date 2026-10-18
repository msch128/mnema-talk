import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'

// What the Talk's strip and grid of participant tiles are given and say.
export const talkTileProps = {
  users: { type: Array as PropType<TalkParticipant[]>, required: true as const },
  streamOf: { type: Function as PropType<(user: TalkParticipant) => MediaStream | null>, required: true as const },
  canFocusCamera: { type: Function as PropType<(user: TalkParticipant) => boolean>, required: true as const },
  isCameraFocused: { type: Function as PropType<(user: TalkParticipant) => boolean>, required: true as const }
}

export interface TalkTileEmits {
  (e: 'focus-camera', user: TalkParticipant): void
  (e: 'fullscreen-camera', userId: string): void
  (e: 'watch-stream', userId: string): void
  (e: 'menu', event: MouseEvent | KeyboardEvent, user: TalkParticipant): void
}
