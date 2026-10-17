import { useMemo } from 'react'
import { useNow } from '../hooks'
import {
  agentRows, chartModel, dearestRuns, heroSub, modelRows, projectOfRepo, projectRows, usageWindow,
} from '../model/spend'
import { useSnapshot } from '../state/context'
import { DEAREST_LIMIT, useDearest } from '../state/dearest'
import { useProjectList } from '../state/projects'
import { useUsagePage } from '../state/usagePage'
import { DearestList, HoldsCard, ResetNote, SpendCard, SpendChart, SpendHero, UsageError, UsageLoading } from '../components/UsageView'
import './Usage.css'

/**
 * The usage view: today's spend against the daily budget, the 14-day chart, By project /
 * By agent / By model cards from the 14 days' usage, the quota holds, and the most
 * expensive runs. A wide view with no side panel.
 */
export function UsagePage({ now: nowProp }: { now?: number } = {}) {
  const ticking = useNow(30_000)
  const now = nowProp ?? ticking
  const snap = useSnapshot()
  const { days, stats, totals, daysError, holds } = useUsagePage(now)
  const projects = useProjectList()
  const win = useMemo(() => usageWindow(now), [now])
  const dearest = useDearest(new Date(`${win.from}T00:00:00`).toISOString())

  const spend = snap.spend
  const sub = useMemo(() => (spend ? heroSub(spend) : { of: '', note: '' }), [spend])
  const ofRepo = useMemo(() => projectOfRepo(projects.data?.projects ?? []), [projects.data])
  const agents = useMemo(() => agentRows(stats.data), [stats.data])
  const models = useMemo(() => modelRows(stats.data), [stats.data])
  const projs = useMemo(() => projectRows(stats.data, ofRepo), [stats.data, ofRepo])
  const chart = useMemo(() => chartModel(days, totals, spend?.budget ?? 0), [days, totals, spend])
  const runs = useMemo(() => dearestRuns(dearest.runs, DEAREST_LIMIT), [dearest.runs])
  const stale = snap.error !== null

  const error = stats.error ?? daysError ?? dearest.error ?? projects.error ?? holds.error
  const loading = !spend && !stats.data && (stats.loading || dearest.loading)

  return (
    <div className="page page-wide usage-page">
      <header className="page-head">
        <h1>Usage</h1>
      </header>

      {loading && <UsageLoading />}
      {!loading && !spend && <UsageError error={snap.error ?? error ?? 'the daemon did not answer'} />}
      {!loading && spend && (
        <>
          <SpendHero usdToday={spend.usd} budget={spend.budget} sub={sub} offline={stale} />
          <ResetNote />
          {error && <UsageError error={error} />}
          <SpendChart bars={chart.bars} max={chart.max} cap={spend.budget} />
          <div className="sh-spend-cards">
            <SpendCard title="By project" rows={projs} empty="No project spend in the last 14 days." />
            <SpendCard title="By agent" rows={agents} empty="No agent spend in the last 14 days." />
            <SpendCard title="By model" rows={models} empty="No model spend in the last 14 days." />
          </div>
          <HoldsCard holds={holds.data ?? undefined} now={now} />
          <DearestList runs={runs} now={now} />
        </>
      )}
    </div>
  )
}
