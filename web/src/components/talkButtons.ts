// Shared button looks of the Talk view: the header's square buttons and the stage's dark ones.
export const focusRing = 'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent'

export function headerButton(active: boolean) {
  return [
    'relative w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center rounded-md border transition-colors motion-reduce:transition-none',
    focusRing,
    active
      ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent'
      : 'border-mnema-hairline bg-mnema-surface text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover'
  ]
}

export const overlayButton = 'w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center rounded-lg text-white transition-colors motion-reduce:transition-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/80'
