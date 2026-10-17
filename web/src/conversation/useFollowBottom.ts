import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'

/** How close to the bottom, in px, still counts as being at it. */
const NEAR_BOTTOM = 24

/** Scrolls an element to its end and returns where it landed. */
function scrollToEnd(box: HTMLElement): number {
  box.scrollTop = box.scrollHeight
  return box.scrollTop
}

/** Moves the nearest scroller of `target` (the window when none) to its end, noting where it landed. */
function scrollToBottom(target: RefObject<HTMLElement | null>, lastY: { current: number }) {
  const box = ownScroller(target.current)
  if (box) {
    lastY.current = scrollToEnd(box)
    return
  }
  const el = document.documentElement
  window.scrollTo({ top: el.scrollHeight })
  lastY.current = Math.max(0, el.scrollHeight - window.innerHeight)
}

/**
 * The nearest ancestor that scrolls on its own, else null for the page (the window). An
 * ancestor counts only while its content overflows it: a wrapper that is auto but grows
 * with its content leaves the scrolling to whatever is outside it.
 */
function ownScroller(el: HTMLElement | null): HTMLElement | null {
  for (let p = el?.parentElement; p; p = p.parentElement) {
    const y = window.getComputedStyle(p).overflowY
    if ((y === 'auto' || y === 'scroll') && p.scrollHeight > p.clientHeight) return p
  }
  return null
}

/**
 * Keeps the page at the bottom of a growing thread while the reader is there, and leaves
 * them alone once they scroll up. The same behaviour as the chat page's hook, kept here
 * so the two pages do not depend on each other. It scrolls only the nearest scrollable
 * ancestor of `target` (the window when none), never the page around a side panel: the
 * reader leaves by scrolling up (the offset falls, or the wheel goes up) and returns by reaching the bottom, while our
 * own scrolling and content growth never move the offset up. `deps` re-pins on our own
 * changes, and a ResizeObserver on `target` on content that grows by itself.
 */
export function useFollowBottom(target: RefObject<HTMLElement | null>, deps: unknown[]) {
  const pinned = useRef(true)
  const lastY = useRef(0)
  const [following, setFollowing] = useState(true)

  const pin = useCallback((v: boolean) => {
    if (pinned.current !== v) {
      pinned.current = v
      setFollowing(v)
    }
  }, [])
  const toBottom = useCallback(() => scrollToBottom(target, lastY), [target])
  const jump = useCallback(() => {
    pin(true)
    toBottom()
  }, [pin, toBottom])

  useEffect(() => {
    // The scroller is found again on each event: a wrapper starts to scroll once the thread outgrows it.
    const onScroll = (e: Event) => {
      const box = ownScroller(target.current)
      const from = e.target
      if (box ? from !== box : from instanceof Element) return
      const y = box ? box.scrollTop : window.scrollY
      const atEnd = box ? box.clientHeight + y >= box.scrollHeight - NEAR_BOTTOM : window.innerHeight + y >= document.documentElement.scrollHeight - NEAR_BOTTOM
      if (atEnd) pin(true)
      else if (y < lastY.current) pin(false)
      lastY.current = y
    }
    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) pin(false)
    }
    const onResize = () => {
      if (pinned.current) toBottom()
    }
    const first = ownScroller(target.current)
    lastY.current = first ? first.scrollTop : window.scrollY
    // Scroll does not bubble: listen in the capture phase to hear whichever element scrolls.
    window.addEventListener('scroll', onScroll, { passive: true, capture: true })
    window.addEventListener('wheel', onWheel, { passive: true })
    window.addEventListener('resize', onResize)
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(onResize)
    if (target.current) ro?.observe(target.current)
    return () => {
      window.removeEventListener('scroll', onScroll, { capture: true })
      window.removeEventListener('wheel', onWheel)
      window.removeEventListener('resize', onResize)
      ro?.disconnect()
    }
  }, [target, pin, toBottom])

  useEffect(() => {
    if (pinned.current) toBottom()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toBottom, ...deps])

  return { following, jump }
}
