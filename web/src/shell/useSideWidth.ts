import { useCallback, useEffect, useMemo, useState, type RefObject } from 'react'
import { clampSide, defaultSide, measureCh, readSide, sideLimits, writeSide, type Limits } from '../ui/sideWidth'

/**
 * The side panel's width for a view: the one remembered for it in localStorage, else the
 * design's default, always inside the limits (the main column keeps 40 characters). A drag
 * moves it live (`resize`) and remembers it on release (`commit`); `reset` forgets it.
 */
export function useSideWidth(view: string, sidebar: RefObject<HTMLElement | null>) {
  const [mine, setMine] = useState<{ view: string; saved: number | null; live: number | null }>(() => ({
    view,
    saved: readSide(window.localStorage, view),
    live: null,
  }))
  const [win, setWin] = useState(() => window.innerWidth)
  if (mine.view !== view) setMine({ view, saved: readSide(window.localStorage, view), live: null })
  useEffect(() => {
    const on = () => setWin(window.innerWidth)
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [])

  const ch = useMemo(() => measureCh(), [])
  const rem = useMemo(() => parseFloat(getComputedStyle(document.documentElement).fontSize) || 16, [])
  // Read at the moment of use: the sidebar's width is a layout fact, not state.
  const limits = (): Limits => {
    const total = document.documentElement.clientWidth || window.innerWidth
    return sideLimits({ win: total, sidebar: sidebar.current?.offsetWidth ?? 0, rem, ch })
  }

  const width = clampSide(mine.live ?? mine.saved ?? defaultSide(win, rem), limits())
  const resize = useCallback((px: number) => setMine((m) => ({ ...m, live: px })), [])
  const commit = useCallback(
    (px: number) => {
      writeSide(window.localStorage, view, px)
      setMine((m) => ({ ...m, saved: px, live: null }))
    },
    [view],
  )
  const reset = useCallback(() => {
    writeSide(window.localStorage, view, null)
    setMine((m) => ({ ...m, saved: null, live: null }))
  }, [view])
  return { width, limits, resize, commit, reset }
}
