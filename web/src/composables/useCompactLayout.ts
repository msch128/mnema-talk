import { ref } from 'vue'

// Phone layout: below Tailwind's `md` breakpoint (768px) the channel list is a
// drawer and the thread and member list are full-screen panels. The same
// number is behind every `max-md:` class, so CSS and script agree.
const COMPACT_QUERY = '(max-width: 767px)'

const isCompact = ref(false)
const navOpen = ref(false)
let started = false

/** Keeps the layout height equal to what is visible while an on-screen keyboard is up (iOS does not resize the page). */
function trackVisualViewport() {
  const vv = window.visualViewport
  if (!vv) return
  const root = document.documentElement
  const update = () => {
    const covered = isCompact.value && window.innerHeight - vv.height > 80
    if (covered) {
      root.style.setProperty('--app-h', `${Math.round(vv.height)}px`)
      window.scrollTo(0, 0)
    } else {
      root.style.removeProperty('--app-h')
    }
  }
  vv.addEventListener('resize', update)
  vv.addEventListener('scroll', update)
}

function start() {
  if (started || typeof window === 'undefined' || !window.matchMedia) return
  started = true
  const query = window.matchMedia(COMPACT_QUERY)
  isCompact.value = query.matches
  query.addEventListener('change', (event) => {
    isCompact.value = event.matches
    if (!event.matches) navOpen.value = false
  })
  trackVisualViewport()
}

/** Whether the window is phone-sized, and the state of the channel drawer. */
export function useCompactLayout() {
  start()
  return {
    isCompact,
    navOpen,
    openNav: () => { navOpen.value = true },
    closeNav: () => { navOpen.value = false },
  }
}
