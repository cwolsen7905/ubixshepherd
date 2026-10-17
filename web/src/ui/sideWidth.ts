// The side panel's width: its limits, default, the snap past 60% and the per-view memory.

/** The main column keeps at least this many characters. */
export const MAIN_MIN_CH = 40
/** Dragging the side past this share of the window offers to swap the layouts. */
export const SNAP_FRACTION = 0.6
/** An arrow key moves the divider this far; with Shift, the big step. */
export const STEP = 16
export const BIG_STEP = 64

const KEY = 'shepherd.sidewidth.'

export interface Limits {
  min: number
  max: number
}

/** The design's side column: 38% of the window, from 20rem to 36rem. */
export function defaultSide(win: number, rem: number): number {
  return Math.min(Math.max(0.38 * win, 20 * rem), 36 * rem)
}

/** Side runs from 20rem to what leaves the main column its 40 characters. */
export function sideLimits({ win, sidebar, rem, ch }: { win: number; sidebar: number; rem: number; ch: number }): Limits {
  const min = 20 * rem
  return { min, max: Math.max(min, win - sidebar - MAIN_MIN_CH * ch) }
}

export function clampSide(px: number, l: Limits): number {
  return Math.round(Math.min(Math.max(px, l.min), l.max))
}

/** Past 60% of the window the release swaps the layouts instead of resizing. */
export function isSnap(side: number, win: number): boolean {
  return side > SNAP_FRACTION * win
}

/** A character's width in the body font, measured; a rough 0.55em where nothing is laid out. */
export function measureCh(): number {
  const probe = document.createElement('span')
  probe.style.cssText = 'position:absolute;visibility:hidden;width:100ch;height:0'
  document.body.appendChild(probe)
  const w = probe.getBoundingClientRect().width / 100
  probe.remove()
  if (w > 0) return w
  return 0.55 * (parseFloat(getComputedStyle(document.body).fontSize) || 14)
}

/** The width remembered for a view, or null. */
export function readSide(store: Pick<Storage, 'getItem'>, view: string): number | null {
  try {
    const n = Number(store.getItem(KEY + view))
    return Number.isFinite(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

export function writeSide(store: Pick<Storage, 'setItem' | 'removeItem'>, view: string, px: number | null) {
  try {
    if (px === null) store.removeItem(KEY + view)
    else store.setItem(KEY + view, String(Math.round(px)))
  } catch {
    // Storage can be full or blocked: the width then lasts until the page reloads.
  }
}
