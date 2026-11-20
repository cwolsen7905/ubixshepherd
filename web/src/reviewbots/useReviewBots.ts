import { useCallback, useEffect, useState } from 'react'
import { reviewBots } from '../api/client'
import type { ReviewBots } from '../api/types'

/** How often the page asks again: the daemon caches for about as long, and the forge is slow. */
const REFRESH_MS = 5 * 60_000

export interface ReviewBotsState {
  data: ReviewBots | null
  error: string | null
  loading: boolean
  /** Asks again now; with `forge` the daemon reads the forge instead of its cache. */
  reload: (forge?: boolean) => void
}

/** Loads the review bots report, keeping the last answer on show while the next loads and after a failure. */
export function useReviewBots(): ReviewBotsState {
  const [tick, setTick] = useState({ n: 0, forge: false })
  const [state, setState] = useState<Omit<ReviewBotsState, 'reload'>>({ data: null, error: null, loading: true })

  useEffect(() => {
    const id = window.setInterval(() => setTick((t) => ({ n: t.n + 1, forge: false })), REFRESH_MS)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    let live = true
    reviewBots(tick.forge).then(
      (data) => live && setState({ data, error: null, loading: false }),
      (e: unknown) => live && setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e), loading: false })),
    )
    return () => {
      live = false
    }
  }, [tick])

  const reload = useCallback((forge = false) => {
    setState((s) => ({ ...s, loading: true }))
    setTick((t) => ({ n: t.n + 1, forge }))
  }, [])
  return { ...state, reload }
}
