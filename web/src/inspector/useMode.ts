import { useEffect, useState } from 'react'

/** How the inspector sits: beside the page, over it as a drawer, or as a bottom sheet. */
export type Mode = 'split' | 'overlay' | 'sheet'

export const SPLIT_MIN = 1440
export const SHEET_MAX = 760

/** The mode for a window width: split at 1440 and up, a sheet below 760, an overlay between. */
export function modeFor(width: number): Mode {
  if (width >= SPLIT_MIN) return 'split'
  if (width < SHEET_MAX) return 'sheet'
  return 'overlay'
}

export function useMode(): Mode {
  const [mode, setMode] = useState<Mode>(() => modeFor(window.innerWidth))
  useEffect(() => {
    const on = () => setMode(modeFor(window.innerWidth))
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [])
  return mode
}
