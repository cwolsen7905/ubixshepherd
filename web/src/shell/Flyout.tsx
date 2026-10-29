import { forwardRef, useLayoutEffect, useState, type HTMLAttributes, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

const GAP = 8
const EDGE = 8

type Pos = { left: number; top: number; side: 'right' | 'left' }

/**
 * Where a flyout of this size goes beside `anchor`: to its right, flipped to its left when the
 * right edge of the window would cut it, and slid up or down to stay inside the window.
 */
export function placeFlyout(anchor: DOMRect, size: { width: number; height: number }, view: { width: number; height: number }): Pos {
  let side: Pos['side'] = 'right'
  let left = anchor.right + GAP
  if (left + size.width > view.width - EDGE && anchor.left - GAP - size.width >= EDGE) {
    side = 'left'
    left = anchor.left - GAP - size.width
  }
  left = Math.max(EDGE, Math.min(left, view.width - EDGE - size.width))
  // Level with the anchor's bottom, as the old inline card was, then kept inside the window.
  const top = Math.max(EDGE, Math.min(anchor.bottom - size.height, view.height - EDGE - size.height))
  return { left, top, side }
}

/**
 * A hover card on a layer above the columns: drawn into the document body, fixed beside
 * its anchor, so a sidebar's overflow or stacking never clips it. Reusable for any sidebar hover.
 * The anchor stays the owner of open and close; this only places and draws.
 */
export const Flyout = forwardRef<HTMLDivElement, { anchor: HTMLElement | null; children: ReactNode } & Omit<HTMLAttributes<HTMLDivElement>, 'children'>>(
  function Flyout({ anchor, children, className, ...rest }, ref) {
    const [el, setEl] = useState<HTMLDivElement | null>(null)
    const [pos, setPos] = useState<Pos | null>(null)

    useLayoutEffect(() => {
      if (!anchor || !el) return
      const place = () => setPos(placeFlyout(anchor.getBoundingClientRect(), { width: el.offsetWidth, height: el.offsetHeight }, { width: window.innerWidth, height: window.innerHeight }))
      place()
      window.addEventListener('resize', place)
      window.addEventListener('scroll', place, true)
      return () => {
        window.removeEventListener('resize', place)
        window.removeEventListener('scroll', place, true)
      }
    }, [anchor, el, children])

    if (!anchor) return null
    return createPortal(
      <div
        {...rest}
        ref={(node) => {
          setEl(node)
          if (typeof ref === 'function') ref(node)
          else if (ref) ref.current = node
        }}
        className={className ? `sh-flyout ${className}` : 'sh-flyout'}
        data-component="Flyout"
        data-side={pos?.side}
        style={{ left: pos?.left ?? 0, top: pos?.top ?? 0, visibility: pos ? undefined : 'hidden' }}
      >
        {children}
      </div>,
      document.body,
    )
  },
)
