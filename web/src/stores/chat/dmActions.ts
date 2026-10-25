// The direct message actions the chat store offers, bound to its state.
import { addDmMember, dmById, dmChannel, fetchDms, isDm, leaveDm, openDm, setDmHidden, startDm, waitForDms } from './dms'
import { moveFavourite, saveDmSettings, setFavourites, toggleFavourite } from './dmPrefs'
import type { Cx } from './state'

export function dmActions(cx: Cx) {
  const bind = <A extends unknown[], R>(fn: (cx: Cx, ...args: A) => R) => (...args: A): R => fn(cx, ...args)
  return {
    isDm: bind(isDm),
    dmById: bind(dmById),
    dmChannel: bind(dmChannel),
    fetchDms: bind(fetchDms),
    waitForDms: bind(waitForDms),
    openDm: bind(openDm),
    startDm: bind(startDm),
    addDmMember: bind(addDmMember),
    leaveDm: bind(leaveDm),
    setDmHidden: bind(setDmHidden),
    setFavourites: bind(setFavourites),
    toggleFavourite: bind(toggleFavourite),
    moveFavourite: bind(moveFavourite),
    saveDmSettings: bind(saveDmSettings)
  }
}
