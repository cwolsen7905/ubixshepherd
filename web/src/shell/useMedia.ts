import { useEffect, useState } from 'react'

/** A media query's current answer, kept up to date. False where matchMedia is missing. */
export function useMedia(query: string): boolean {
  const read = () => (typeof window.matchMedia === 'function' ? window.matchMedia(query).matches : false)
  const [on, setOn] = useState(read)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia(query)
    const update = () => setOn(mq.matches)
    update()
    mq.addEventListener('change', update)
    return () => mq.removeEventListener('change', update)
  }, [query])
  return on
}

/** Below this the sidebar becomes a sheet behind a menu button; from it up the sidebar is always there. */
export const SHEET_MAX = 759

export type ShellMode = 'full' | 'sheet'

export function useShellMode(): ShellMode {
  return useMedia(`(max-width: ${SHEET_MAX}px)`) ? 'sheet' : 'full'
}
