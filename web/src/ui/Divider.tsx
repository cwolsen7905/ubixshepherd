import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type PointerEvent } from 'react'
import { BIG_STEP, clampSide, isSnap, STEP, type Limits } from './sideWidth'
import { cx } from './cx'

const onHash = (fn: () => void) => {
  window.addEventListener('hashchange', fn)
  return () => window.removeEventListener('hashchange', fn)
}

/** Whether the address has an item open full width (`open=kind:id/full`), however the slash is encoded. */
export function openFull(hash: string): boolean {
  let h = hash
  try {
    h = decodeURIComponent(hash)
  } catch {
    // A malformed escape: match the hash as written.
  }
  return /[?&]open=[^&]*\/full(?:&|$)/.test(h)
}

/**
 * The divider between the main column and the side panel (`.sh-divider`): an 11px hit area with
 * a rule that thickens to lichen on hover, focus and while dragging. Drag, or use the arrow keys
 * (Shift for a big step); double-click resets. `width` is the side panel's width; the parent
 * applies it. Dragging past 60% of the window shows a wash and, on release, calls `onSwap`
 * instead of resizing; dragging back before releasing cancels. Not rendered under 768px.
 */
export function Divider({
  width,
  limits,
  onResize,
  onCommit,
  onReset,
  onSwap,
  swapLabel = 'Release to open as page',
  right = 0,
}: {
  width: number
  limits: () => Limits
  /** Live while dragging or after a key. */
  onResize: (px: number) => void
  /** The width to remember: on release and after a key. */
  onCommit: (px: number) => void
  onReset: () => void
  /** Release past the snap point. The layouts swap; the width is left as it was. */
  onSwap?: () => void
  swapLabel?: string
  /** Space to the right of the side panel (a scrollbar). */
  right?: number
}) {
  const full = useSyncExternalStore(onHash, () => openFull(window.location.hash))
  const [drag, setDrag] = useState<{ x: number; snap: boolean } | null>(null)
  const start = useRef(width)
  const cleanup = useRef<(() => void) | null>(null)
  useEffect(() => () => cleanup.current?.(), [])

  const win = () => document.documentElement.clientWidth || window.innerWidth

  const down = (e: PointerEvent) => {
    if (e.button !== 0) return
    e.preventDefault()
    start.current = width
    setDrag({ x: e.clientX, snap: false })
    let last = width
    let snap = false
    const move = (ev: globalThis.PointerEvent) => {
      const raw = win() - right - ev.clientX
      snap = isSnap(raw, win())
      last = clampSide(raw, limits())
      onResize(snap ? start.current : last)
      setDrag({ x: ev.clientX, snap })
    }
    const up = () => {
      stop()
      setDrag(null)
      if (snap) {
        onResize(start.current)
        onSwap?.()
      } else {
        onCommit(last)
      }
    }
    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      cleanup.current = null
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
    cleanup.current = stop
  }

  const key = (e: KeyboardEvent) => {
    const step = e.shiftKey ? BIG_STEP : STEP
    // The divider is the panel's left edge: left makes the panel wider.
    const to = e.key === 'ArrowLeft' ? width + step : e.key === 'ArrowRight' ? width - step : null
    if (to === null) return
    e.preventDefault()
    const next = clampSide(to, limits())
    onResize(next)
    onCommit(next)
  }

  // An item open full width has no side panel to size: its rule would run through the page.
  if (full) return null

  const l = limits()
  return (
    <>
      <div
        className={cx('sh-divider', drag && 'is-dragging')}
        data-component="Divider"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the side column"
        aria-valuemin={Math.round(l.min)}
        aria-valuemax={Math.round(l.max)}
        aria-valuenow={Math.round(width)}
        tabIndex={0}
        title="Drag to resize. Double-click to reset."
        style={{ right: width + right }}
        onPointerDown={down}
        onDoubleClick={onReset}
        onKeyDown={key}
      >
        <i />
      </div>
      {drag?.snap && (
        <div className="sh-snap" style={{ width: win() - drag.x - right, right }}>
          <b>{swapLabel}</b>
        </div>
      )}
    </>
  )
}
