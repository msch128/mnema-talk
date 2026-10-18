

// The SFU publishes a camera under this prefix plus the user ID as stream ID,
// and the sound of a screen share under SCREEN_AUDIO_STREAM_PREFIX (screen
// share and voice use the plain user ID); see internal/sfu.
export const CAMERA_STREAM_PREFIX = 'cam:'
export const SCREEN_AUDIO_STREAM_PREFIX = 'screen:'

export function publisherId(streamId: string | undefined): string | null {
  if (!streamId) return null
  const id = streamId.startsWith(CAMERA_STREAM_PREFIX)
    ? streamId.slice(CAMERA_STREAM_PREFIX.length)
    : streamId.startsWith(SCREEN_AUDIO_STREAM_PREFIX)
      ? streamId.slice(SCREEN_AUDIO_STREAM_PREFIX.length)
      : streamId
  return id || null
}
