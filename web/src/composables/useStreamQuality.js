// The streamer's quality menu (the gear on my own share), like Discord's:
// stream mode, resolution and frame rate, the stream's sound, and what is
// actually being sent. The choice lives in the voice store for the current
// share; useWebRTC applies it live (see lib/streamQuality).
import { computed, ref, getCurrentInstance, onBeforeUnmount } from 'vue'
import { useVoiceStore } from '../stores/voice'
import {
  normalizeQuality, streamModeOf, STREAM_PRESETS, STREAM_RESOLUTIONS, STREAM_FRAME_RATES,
  summarizeSendStats, formatSendStats
} from '../lib/streamQuality'
import { t } from '../i18n'

// How often the "Advanced" info refreshes while it is open.
export const STATS_INTERVAL_MS = 1000

export function resolutionLabel(r) {
  return r === 'source' ? t('talk.quality.source') : `${r}p`
}

export function frameRateLabel(fps) {
  return t('talk.quality.fps', { fps })
}

/**
 * getStats: async () => RTCStatsReport values of the screen sender (or null);
 * see useWebRTC().getScreenSendStats.
 */
export function useStreamQuality({ getStats } = {}) {
  const voiceStore = useVoiceStore()
  const quality = computed(() => normalizeQuality(voiceStore.screenQuality))
  const mode = computed(() => streamModeOf(voiceStore.screenQuality))

  function setMode(m) {
    // The presets set both values; Custom keeps the current ones.
    if (STREAM_PRESETS[m]) voiceStore.setScreenQuality({ ...STREAM_PRESETS[m] })
    else if (m === 'custom') voiceStore.setScreenQuality({ ...quality.value, custom: true })
  }
  // A value that matches no preset makes the mode Custom (see streamModeOf).
  function setResolution(resolution) {
    voiceStore.setScreenQuality({ ...quality.value, resolution })
  }
  function setFrameRate(fps) {
    voiceStore.setScreenQuality({ ...quality.value, fps })
  }

  // --- What is being sent, sampled while "Advanced" is open ---
  const sendStats = ref(null)
  let timer = null
  let previous = null
  let sampling = 0

  async function sample() {
    const run = sampling
    let raw
    try {
      raw = await getStats?.()
    } catch {
      raw = null
    }
    if (run !== sampling) return
    const summary = raw ? summarizeSendStats(raw, previous) : null
    if (summary) previous = summary.sample
    sendStats.value = summary
  }

  function startStats() {
    stopStats()
    timer = setInterval(sample, STATS_INTERVAL_MS)
    return sample()
  }

  function stopStats() {
    sampling++
    if (timer) clearInterval(timer)
    timer = null
    previous = null
  }

  if (getCurrentInstance()) onBeforeUnmount(stopStats)

  /** ContextMenu items; read reactively, so the open menu follows every change. */
  function menuItems({ onChangeSource } = {}) {
    const q = quality.value
    const m = mode.value
    const shown = formatSendStats(sendStats.value)
    const modeItem = (id, subtitle) => ({
      id: `mode-${id}`,
      type: 'radio',
      label: t(`talk.quality.${id}`),
      subtitle,
      checked: m === id,
      keepOpen: true,
      action: () => setMode(id)
    })
    const items = [
      { type: 'label', label: t('talk.quality.mode') },
      modeItem('gaming', t('talk.quality.gamingHint')),
      modeItem('screen', t('talk.quality.screenHint')),
      modeItem('custom'),
      { type: 'separator' },
      {
        id: 'resolution',
        type: 'submenu',
        label: t('talk.quality.resolution'),
        value: resolutionLabel(q.resolution),
        minWidth: 150,
        items: STREAM_RESOLUTIONS.map(r => ({
          id: `resolution-${r}`,
          type: 'radio',
          label: resolutionLabel(r),
          checked: q.resolution === r,
          keepOpen: true,
          action: () => setResolution(r)
        }))
      },
      {
        id: 'fps',
        type: 'submenu',
        label: t('talk.quality.frameRate'),
        value: frameRateLabel(q.fps),
        minWidth: 150,
        items: STREAM_FRAME_RATES.map(fps => ({
          id: `fps-${fps}`,
          type: 'radio',
          label: frameRateLabel(fps),
          checked: q.fps === fps,
          keepOpen: true,
          action: () => setFrameRate(fps)
        }))
      },
      { type: 'separator' },
      {
        id: 'mute-sound',
        type: 'checkbox',
        label: t('talk.quality.muteSound'),
        checked: !!voiceStore.isScreenAudioMuted,
        disabled: !voiceStore.hasScreenAudio,
        keepOpen: true,
        action: () => voiceStore.toggleScreenAudioMute()
      },
      {
        id: 'advanced',
        type: 'submenu',
        label: t('talk.quality.advanced'),
        minWidth: 220,
        onOpen: () => { startStats() },
        onClose: stopStats,
        items: [
          { id: 'codec', type: 'info', label: t('talk.quality.codec'), value: shown.codec },
          { id: 'size', type: 'info', label: t('talk.quality.resolution'), value: shown.resolution },
          { id: 'fps', type: 'info', label: t('talk.quality.frameRate'), value: shown.fps },
          { id: 'bitrate', type: 'info', label: t('talk.quality.bitrate'), value: shown.bitrate }
        ]
      }
    ]
    if (onChangeSource) {
      items.push({ type: 'separator' })
      items.push({ id: 'change-source', label: t('talk.quality.changeSource'), action: onChangeSource })
    }
    return items
  }

  return { quality, mode, setMode, setResolution, setFrameRate, sendStats, startStats, stopStats, menuItems }
}
