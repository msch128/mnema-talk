/// <reference lib="esnext.disposable" />
/// <reference types="vite/client" />
import type { t, locale } from './i18n/index.ts'

declare global {
  const __APP_VERSION__: string
  // Optional browser extensions used by capability checks / requested hints.
  // These declarations do not promise that an engine supports an extension.
  interface Window { webkitAudioContext?: typeof AudioContext }
  interface RTCConfiguration { dscp?: boolean }
  interface MediaTrackSupportedConstraints { restrictOwnAudio?: boolean }
  interface MediaTrackSettings { restrictOwnAudio?: boolean }
  interface MediaTrackConstraints { restrictOwnAudio?: ConstrainBoolean }
  interface RTCRtpEncodingParameters { networkPriority?: RTCPriorityType }
}

declare module 'vue' {
  interface ComponentCustomProperties {
    $t: typeof t
    $i18nLocale: typeof locale
  }
}
export {}
