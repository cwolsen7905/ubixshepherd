import { useMemo } from 'react'
import { useNow } from '../hooks'
import {
  agentRows, chartModel, costliestRuns, heroSub, modelRows, projectOfRepo, projectRows, runsInRange,
} from '../model/spend'
import { DEFAULT_USAGE_RANGE, usageDays } from '../model/usage'
import type { UsageRange } from '../router'
import { useSnapshot } from '../state/context'
import { COSTLIEST_LIMIT, useCostliest } from '../state/costliest'
import { useProjectList } from '../state/projects'
import { useUsagePage } from '../state/usagePage'
import { CostliestList, HoldsCard, ResetNote, SpendCard, SpendChart, SpendHero, UsageError, UsageLoading } from '../components/UsageView'
import { SummaryCards, TokensSection, UsageRangeControl } from '../components/UsageViewDetail'
import './Usage.css'

/**
 * The usage view: one range control over everything. Spend: today against the daily
 * budget, the four summary cards, the daily chart, By project / By agent / By model, the
 * quota holds and the most expensive runs. Then tokens and context: peak context against
 * 200k and the token tables. A wide view with no side panel.
 */
export function UsagePage({ now: nowProp, range = DEFAULT_USAGE_RANGE, from, to }: { now?: number; range?: UsageRange; from?: string; to?: string } = {}) {
  const ticking = useNow(30_000)
  const now = nowProp ?? ticking
  const snap = useSnapshot()
  const win = useMemo(() => usageDays(range, now, from, to), [range, now, from, to])
  const { days, stats, totals, daysError, holds } = useUsagePage(win)
  const projects = useProjectList()
  const costliest = useCostliest(new Date(`${win.from}T00:00:00`).toISOString())

  const spend = snap.spend
  const sub = useMemo(() => (spend ? heroSub(spend) : { of: '', note: '' }), [spend])
  const ofRepo = useMemo(() => projectOfRepo(projects.data?.projects ?? []), [projects.data])
  const agents = useMemo(() => agentRows(stats.data), [stats.data])
  const models = useMemo(() => modelRows(stats.data), [stats.data])
  const projs = useMemo(() => projectRows(stats.data, ofRepo), [stats.data, ofRepo])
  const chart = useMemo(() => chartModel(days, totals, spend?.budget ?? 0), [days, totals, spend])
  const runs = useMemo(() => costliestRuns(runsInRange(costliest.runs, win.from, win.to), COSTLIEST_LIMIT), [costliest.runs, win])
  const stale = snap.error !== null

  const error = stats.error ?? daysError ?? costliest.error ?? projects.error ?? holds.error
  const loading = !spend && !stats.data && (stats.loading || costliest.loading)

  return (
    <div className="page page-wide usage-page">
      <header className="page-head">
        <h1>Usage</h1>
      </header>

      <UsageRangeControl range={range} from={win.from} to={win.to} />

      {loading && <UsageLoading />}
      {!loading && !spend && <UsageError error={snap.error ?? error ?? 'the daemon did not answer'} />}
      {!loading && spend && (
        <>
          <SpendHero usdToday={spend.usd} budget={spend.budget} sub={sub} offline={stale} />
          <ResetNote />
          {error && <UsageError error={error} />}
          {stats.data && <SummaryCards stats={stats.data} />}
          <SpendChart bars={chart.bars} max={chart.max} cap={spend.budget} />
          <div className="sh-spend-cards">
            <SpendCard title="By project" rows={projs} empty="No project spend in this range." />
            <SpendCard title="By agent" rows={agents} empty="No agent spend in this range." />
            <SpendCard title="By model" rows={models} empty="No model spend in this range." />
          </div>
          <HoldsCard holds={holds.data ?? undefined} now={now} />
          <CostliestList runs={runs} />
          {stats.data && <TokensSection stats={stats.data} />}
        </>
      )}
    </div>
  )
}
