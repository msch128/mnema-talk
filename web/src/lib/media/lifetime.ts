import { DEFAULT_TIMING, type MediaTiming } from './bounds'

// __MNEMA_E2E_HOOKS__ is true only in unit tests and in the e2e build
// (MNEMA_E2E=1 at build time, see vite.config.ts). A release build replaces it
// with false, so the hook is dead code there and nothing in the page can
// shorten the lifetimes.

/** The lifetimes for a build: the constants, or a test hook's values where the build allows the hook. */
export function resolveTiming(hooksAllowed: boolean, hook: Partial<MediaTiming> | undefined): MediaTiming {
  if (!hooksAllowed || !hook) return DEFAULT_TIMING
  const timing = { ...DEFAULT_TIMING }
  for (const key of Object.keys(timing) as (keyof MediaTiming)[]) {
    const value = hook[key]
    if (typeof value === 'number' && Number.isFinite(value) && value >= 0) timing[key] = value
  }
  return timing
}

export function mediaTiming(): MediaTiming {
  return resolveTiming(__MNEMA_E2E_HOOKS__, (globalThis as { __MNEMA_MEDIA_TIMING__?: Partial<MediaTiming> }).__MNEMA_MEDIA_TIMING__)
}
