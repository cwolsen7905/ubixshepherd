import { useMemo, type ReactNode } from 'react'
import { useNow } from '../hooks'
import { OfflineContext, offlineWords, retryIn, useOffline, type OfflineState } from '../ui'

/**
 * Tells everything under it whether the daemon answers and when it next tries. The clock ticks
 * here, once a second while offline, so only the readers of the context redraw, not the pages.
 */
export function OfflineProvider({ error, downSince, children }: { error: string | null; downSince: number | null; children: ReactNode }) {
  const offline = error !== null
  const now = useNow(offline ? 1000 : 60_000)
  const value = useMemo<OfflineState>(() => ({ offline, retryIn: offline ? retryIn(downSince, now) : null }), [offline, downSince, now])
  return <OfflineContext.Provider value={value}>{children}</OfflineContext.Provider>
}

/** The banner under the bar while the daemon does not answer: why, what is shown, and when it retries. Drafts are kept. */
export function OfflineBanner({ error }: { error: string | null }) {
  const state = useOffline()
  if (error === null) return null
  return (
    <div className="banner sh-banner sh-banner--bad" data-component="OfflineBanner">
      <span role="alert">The daemon did not answer: {error}. Showing what was last loaded;</span>{' '}
      <span>{offlineWords(state).replace(/^Offline, r/, 'r')}.</span>
    </div>
  )
}
