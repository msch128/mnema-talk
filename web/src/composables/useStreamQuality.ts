// The streamer's quality menu (the gear on my own share), like Discord's:
// stream mode, resolution and frame rate, the stream's sound, and what is
// actually being sent. The choice lives in the voice store for the current
// share; useWebRTC applies it live (see lib/streamQuality).
import { computed, ref, getCurrentInstance, onBeforeUnmount } from 'vue'
import { useVoiceStore } from '../stores/voice'
import {
  normalizeQuality, streamModeOf, STREAM_PRESETS, STREAM_RESOLUTIONS, STREAM_FRAME_RATES,
  summarizeSendStats, formatSendStats, formatStreamDiagnostics
} from '../lib/streamQuality'
import { t } from '../i18n'
import type { MediaStat } from '../lib/mediaStats'
import type { StreamResolution, StreamFrameRate, StreamMode, SendStatsSummary, SendStatsSample } from '../lib/streamQuality'

import type { ContextMenuItem } from '../components/menuTypes'

interface StreamQualityOptions {
  getStats?: () => Promise<Iterable<MediaStat> | null> | Iterable<MediaStat> | null
  getCaptureSettings?: () => MediaTrackSettings | null
}

// How often the "Advanced" info refreshes while it is open.
export const STATS_INTERVAL_MS = 1000

export function resolutionLabel(r: StreamResolution) {
  return r === 'source' ? t('talk.quality.source') : `${r}p`
}

export function frameRateLabel(fps: StreamFrameRate) {
  return t('talk.quality.fps', { fps })
}

/** Samples what is being sent while "Advanced" is open. */
function useSendStatsSampler({ getStats, getCaptureSettings }: StreamQualityOptions) {
  const sendStats = ref<SendStatsSummary | null>(null)
  const captureSettings = ref<MediaTrackSettings | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null
  let previous: SendStatsSample | null = null
  let sampling = 0

  async function sample() {
    const run = sampling
    let raw
    try {
      raw = await getStats?.()
    } catch (err) {
      void err
      raw = null
    }
    if (run !== sampling) return
    captureSettings.value = getCaptureSettings?.() || null
    const summary = raw ? summarizeSendStats(raw, previous) : null
    if (summary) previous = summary.sample
    sendStats.value = summary
  }

  function stopStats() {
    sampling++
    if (timer) clearInterval(timer)
    timer = null
    previous = null
  }

  function startStats() {
    stopStats()
    timer = setInterval(sample, STATS_INTERVAL_MS)
    return sample()
  }

  if (getCurrentInstance()) onBeforeUnmount(stopStats)
  return { sendStats, captureSettings, startStats, stopStats }
}

interface RadioItemOpts {
  id: string
  label: string
  checked: boolean
  action: () => void
  subtitle?: string
}

function radioItem(opts: RadioItemOpts): ContextMenuItem {
  return {
    id: opts.id,
    type: 'radio',
    label: opts.label,
    ...(opts.subtitle === undefined ? {} : { subtitle: opts.subtitle }),
    checked: opts.checked,
    keepOpen: true,
    action: opts.action
  }
}

interface QualityActions {
  setMode(m: StreamMode): void
  setResolution(r: StreamResolution): void
  setFrameRate(fps: StreamFrameRate): void
}

function resolutionAndRateItems(q: { resolution: StreamResolution; fps: StreamFrameRate }, actions: QualityActions): ContextMenuItem[] {
  return [
    {
      id: 'resolution',
      type: 'submenu',
      label: t('talk.quality.resolution'),
      value: resolutionLabel(q.resolution),
      minWidth: 150,
      items: STREAM_RESOLUTIONS.map(r => radioItem({ id: `resolution-${r}`, label: resolutionLabel(r), checked: q.resolution === r, action: () => actions.setResolution(r) }))
    },
    {
      id: 'fps',
      type: 'submenu',
      label: t('talk.quality.frameRate'),
      value: frameRateLabel(q.fps),
      minWidth: 150,
      items: STREAM_FRAME_RATES.map(fps => radioItem({ id: `fps-${fps}`, label: frameRateLabel(fps), checked: q.fps === fps, action: () => actions.setFrameRate(fps) }))
    }
  ]
}

/** The read-only rows of "Advanced": what is actually being sent and captured. */
function advancedInfoItems(fps: StreamFrameRate, sendStats: SendStatsSummary | null, captureSettings: MediaTrackSettings | null): ContextMenuItem[] {
  const shown = formatSendStats(sendStats)
  const diagnosis = formatStreamDiagnostics(sendStats, captureSettings)
  const limitation = sendStats?.limitation
  const efficient = sendStats?.efficientEncoder
  const info = (id: string, label: string, value: string): ContextMenuItem => ({ id, type: 'info', label: t(label), value })
  return [
    info('codec', 'talk.quality.codec', shown.codec),
    info('codec-profile', 'talk.quality.codecProfile', diagnosis.codecProfile),
    info('target-fps', 'talk.quality.targetFps', frameRateLabel(fps)),
    info('capture-setting', 'talk.quality.captureSetting', diagnosis.captureSetting),
    info('capture-fps', 'talk.quality.captureFps', diagnosis.captureFps),
    info('size', 'talk.quality.resolution', shown.resolution),
    info('fps', 'talk.quality.encodedFps', shown.fps),
    info('bitrate', 'talk.quality.bitrate', shown.bitrate),
    info('encoder', 'talk.quality.encoder', diagnosis.encoder),
    info('efficient-encoder', 'talk.quality.efficientEncoder', efficient === null || efficient === undefined ? '–' : t(`talk.quality.${efficient ? 'yes' : 'no'}`)),
    info('encode-time', 'talk.quality.encodeTime', diagnosis.encodeTime),
    info('limitation', 'talk.quality.limitation', limitation ? t(`talk.quality.limit_${limitation}`) : '–'),
    { type: 'label', label: t('talk.quality.fpsNote') }
  ]
}

/**
 * getStats: async () => RTCStatsReport values of the screen sender (or null);
 * see useWebRTC().getScreenSendStats.
 */
export function useStreamQuality(options: StreamQualityOptions = {}) {
  const voiceStore = useVoiceStore()
  const quality = computed(() => normalizeQuality(voiceStore.screenQuality))
  const mode = computed(() => streamModeOf(voiceStore.screenQuality))
  const { sendStats, captureSettings, startStats, stopStats } = useSendStatsSampler(options)

  function setMode(m: StreamMode) {
    // The presets set both values; Custom keeps the current ones.
    if (m === 'gaming' || m === 'screen') voiceStore.setScreenQuality({ ...STREAM_PRESETS[m] })
    else if (m === 'custom') voiceStore.setScreenQuality({ ...quality.value, custom: true })
  }
  // A value that matches no preset makes the mode Custom (see streamModeOf).
  function setResolution(resolution: StreamResolution) {
    voiceStore.setScreenQuality({ ...quality.value, resolution })
  }
  function setFrameRate(fps: StreamFrameRate) {
    voiceStore.setScreenQuality({ ...quality.value, fps })
  }

  /** ContextMenu items; read reactively, so the open menu follows every change. */
  function menuItems({ onChangeSource }: { onChangeSource?: () => void } = {}): ContextMenuItem[] {
    const q = quality.value
    const m = mode.value
    const modeItem = (id: StreamMode, subtitle?: string) => radioItem({ id: `mode-${id}`, label: t(`talk.quality.${id}`), checked: m === id, action: () => setMode(id), subtitle })
    const items: ContextMenuItem[] = [
      { type: 'label', label: t('talk.quality.mode') },
      modeItem('gaming', t('talk.quality.gamingHint')),
      modeItem('screen', t('talk.quality.screenHint')),
      modeItem('custom'),
      { type: 'separator' },
      ...resolutionAndRateItems(q, { setMode, setResolution, setFrameRate }),
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
        minWidth: 290,
        onOpen: () => { startStats() },
        onClose: stopStats,
        items: advancedInfoItems(q.fps, sendStats.value, captureSettings.value)
      }
    ]
    if (onChangeSource) {
      items.push({ type: 'separator' })
      items.push({ id: 'stream-settings', label: t('talk.quality.streamModalTitle'), action: () => voiceStore.openScreenShareModal() })
      items.push({ id: 'change-source', label: t('talk.quality.changeSource'), action: onChangeSource })
    }
    return items
  }

  return { quality, mode, setMode, setResolution, setFrameRate, sendStats, startStats, stopStats, menuItems }
}
