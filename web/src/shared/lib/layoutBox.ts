// Where an element sits by layout, without its transform. A popover that is
// still growing out of its trigger (the pop-in scale) paints a smaller box
// than it takes up; code that keeps it on screen must measure the box it
// will have, not the one on screen this frame.
export function layoutLeft(el: HTMLElement): number {
  const parent = el.offsetParent
  return parent ? parent.getBoundingClientRect().left + parent.clientLeft + el.offsetLeft : el.getBoundingClientRect().left
}
