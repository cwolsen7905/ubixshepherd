import { useMemo } from 'react'
import { daemonInfo, usage } from '../api/client'
import type { StatusDetail, UsageStats } from '../api/types'
import { chartDays } from '../model/spend'
import type { Loaded } from './history'
import { useFeedLast, useUsageLoad } from './usageLoad'

/** The 14-day chart's days, oldest first, from now's local day. */
export function useChartDays(now: number): string[] {
  return useMemo(() => chartDays(now), [now])
}

export interface UsagePageData {
  days: string[]
  /** The 14 days' usage for the cards; null while it loads. */
  stats: Loaded<UsageStats>
  /** Each chart day's total; a day whose read failed keeps nothing. */
  totals: Record<string, number>
  daysError: string | null
  /** The daemon's quota holds, when it reports them. */
  holds: Loaded<StatusDetail['quota_holds']>
}

/** The page's reads: the 14 days' usage for the cards, one day's usage per chart bar, and
 * the daemon's quota holds. */
export function useUsagePage(now: number): UsagePageData {
  const feedLast = useFeedLast()
  const days = useChartDays(now)
  const key = days.join(',')
  const stats = useUsageLoad(`${key}:stats`, () => usage(days[0] as string, days[days.length - 1] as string), feedLast)
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
