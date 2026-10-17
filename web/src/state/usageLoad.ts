import { useEffect, useState } from 'react'
import { useSnapshot } from './context'
import type { Loaded } from './history'

/** How often the usage reads reload when the feed is quiet: spend moves without an event. */
const REFRESH_MS = 30_000

/**
 * Loads something for the usage view, again when the key changes, whenever the feed moves
 * (a run ending is new spend) and every half minute. The last answer stays on show while
 * the next loads, and after a failure.
 */
export function useUsageLoad<T>(key: string, load: () => Promise<T>, feedLast: number): Loaded<T> {
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<Loaded<T> & { key: string }>({ data: null, error: null, loading: true, key })

  useEffect(() => {
    const id = window.setInterval(() => setTick((n) => n + 1), REFRESH_MS)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    let current = true
    load().then(
      (data) => current && setState({ data, error: null, loading: false, key }),
      (e: unknown) => current && setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e), loading: false, key })),
    )
    return () => {
      current = false
    }
    // load closes over what key names.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, feedLast, tick])

  if (state.key !== key) return { data: null, error: null, loading: true }
  return state
}

/** The feed's newest id, for the usage reads to follow. */
export function useFeedLast(): number {
  return useSnapshot().feed.at(-1)?.id ?? 0
}
