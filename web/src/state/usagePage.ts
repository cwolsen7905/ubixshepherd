import { useMemo } from 'react'
import { daemonInfo, usage } from '../api/client'
import { chartDays } from '../model/spend'
import { useSnapshot } from '../state/context'
import { useUsageLoad } from '../state/usageLoad'

/** The 14-day chart's days, oldest first, from now's local day. */
export function useChartDays(now: number): string[] {
  return useMemo(() => chartDays(now), [now])
}

/** One hook for the page's reads: the 14 days' usage, each day's total, the quota holds. */
export function useUsagePage(now: number) {
  const snap = useSnapshot()
  const feedLast = snap.feed.at(-1)?.id ?? 0
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
  return { stats, totals, holds }
}
