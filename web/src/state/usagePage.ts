import { useMemo } from 'react'
import { daemonInfo, usage } from '../api/client'
import type { StatusDetail, UsageStats } from '../api/types'
import { rangeChartDays } from '../model/spend'
import type { Loaded } from './history'
import { useFeedLast, useUsageLoad } from './usageLoad'

export interface UsagePageData {
  days: string[]
  /** The range's usage for the cards and tables; null while it loads. */
  stats: Loaded<UsageStats>
  /** Each chart day's total; a day whose read failed keeps nothing. */
  totals: Record<string, number>
  daysError: string | null
  /** The daemon's quota holds, when it reports them. */
  holds: Loaded<StatusDetail['quota_holds']>
}

/** The page's reads: the range's usage for the cards and tables, one day's usage per chart
 * bar, and the daemon's quota holds. */
export function useUsagePage(range: { from: string; to: string }): UsagePageData {
  const feedLast = useFeedLast()
  const days = useMemo(() => rangeChartDays(range.from, range.to), [range.from, range.to])
  const key = `${range.from}:${range.to}`
  const stats = useUsageLoad(`${key}:stats`, () => usage(range.from, range.to), feedLast)
  const perDay = useUsageLoad(
    `${key}:days`,
    () => Promise.all(days.map((day) => usage(day, day).then((s) => ({ day, usd: s.total.cost_usd }), () => null))),
    feedLast,
  )
  const holds = useUsageLoad(`${key}:holds`, () => daemonInfo.status().then((s) => s.detail?.quota_holds), feedLast)
  const totals = useMemo(() => {
    const out: Record<string, number> = {}
    for (const d of perDay.data ?? []) if (d) out[d.day] = d.usd
    return out
  }, [perDay.data])
  return { days, stats, totals, daysError: perDay.error, holds }
}
