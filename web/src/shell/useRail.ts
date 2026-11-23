import { useCallback, useState } from 'react'
import { useKeys } from '../hooks'

/** Where the sidebar's collapsed state is remembered, beside the side panel's widths. */
export const RAIL_KEY = 'shepherd.sidebar.rail'

/** Whether the person last left the sidebar collapsed to its icon rail. */
export function readRail(store: Storage | null): boolean {
  try {
    return store?.getItem(RAIL_KEY) === '1'
  } catch {
    return false
  }
}

export function writeRail(store: Storage | null, on: boolean) {
  try {
    if (on) store?.setItem(RAIL_KEY, '1')
    else store?.removeItem(RAIL_KEY)
  } catch {
    // A browser that refuses storage still collapses, for this visit.
  }
}

/**
 * The sidebar's rail: collapsed to icons or open, remembered in localStorage. `\` toggles it,
 * except while the person is typing or holds a modifier.
 */
export function useRail(): [boolean, () => void] {
  const [rail, setRail] = useState(() => readRail(window.localStorage))
  const toggle = useCallback(() => {
    setRail((on) => {
      writeRail(window.localStorage, !on)
      return !on
    })
  }, [])
  // Backslash, not `[`: the pending bar and the swap bar step back with `[`.
  useKeys({ '\\': toggle }, [toggle])
  return [rail, toggle]
}
