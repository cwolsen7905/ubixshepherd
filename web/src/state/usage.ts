import { useEffect, useState } from 'react'
import { daemonInfo, usage } from '../api/client'
import type { StatusDetail, UsageStats } from '../api/types'
import { chartDays } from '../model/spend'
import { useSnapshot } from '../state/context'

export interface UsageData {
  /** The 14 days' usage for the cards; null while it loads or when it failed. */
  stats: UsageStats | null
  /** Each chart day's total; a day with no record is 0. */
  days: Record<string, number>
  /** What the cards and chart reads said when they went wrong. */
  error: string | null
  /** The daemon's quota holds, when it reports them. */
  holds: StatusDetail['quota_holds']
}

/** How often the chart days reload when the feed is quiet: spend moves without an event. */
const REFRESH_MS = 30_000

/**
 * What the usage view reads that the snapshot does not hold: the 14 days' usage for the
 * cards, one day's usage per chart bar, and the daemon's quota holds. The chart names the
 * last 14 local days ending on now's; a day whose read fails keeps its last total. All
 * reads go again when the feed moves (a run ending is new spend) and every half minute.
 */
export function useUsage(now: number): UsageData {
  const snap = useSnapshot()
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const [tick, setTick] = useState(0)
  const days = chartDays(now)
  const key = days.join(',')
  const [state, setState] = useState<UsageData & { key: string }>({ stats: null, days: {}, error: null, holds: undefined, key })

  useEffect(() => {
    const id = window.setInterval(() => setTick((n) => n + 1), REFRESH_MS)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    let current = true
    const read = async (): Promise<{ stats: UsageStats; days: Record<string, number>; holds: StatusDetail['quota_holds'] }> => {
      const [stats, perDay, detail] = await Promise.all([
        usage(days[0] as string, days[days.length - 1] as string),
        Promise.all(days.map((day) => usage(day, day).then(
          (s) => ({ day, usd: s.total.cost_usd }),
          () => null,
        ))),
        daemonInfo.status().then(
          (s) => s.detail?.quota_holds,
          () => undefined,
        ),
      ])
      const totals: Record<string, number> = {}
      for (const d of perDay) if (d) totals[d.day] = d.usd
      return { stats, days: totals, holds: detail }
    }
    read().then(
      (data) => current && setState((s) => ({ stats: data.stats, days: { ...s.days, ...data.days }, error: null, holds: data.holds, key })),
      (e: unknown) => current && setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e), key })),
    )
    return () => {
      current = false
    }
    // days is named by key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, feedLast, tick])

  if (state.key !== key) return { stats: null, days: {}, error: null, holds: undefined }
  return state
}
