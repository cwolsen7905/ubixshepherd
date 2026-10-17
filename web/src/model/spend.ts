// The usage view's arithmetic: what today's spend says, the card rows, and the 14-day
// chart's days. Pure functions over the daemon's spend, usage and project answers, so the
// page stays a drawing of them. The 14-day usage endpoint feeds the By agent, By model and
// By project cards; the project list only lends the budget caps. The chart reads one day
// of usage per bar: the endpoint answers a single day when from and to name it.

import type { ProjectSummary, RunRecord, SpendToday, UsageGroup, UsageStats } from '../api/types'
import { usd } from '../format'

/** The chart's window: the last 14 days, today included, oldest first. */
export const CHART_DAYS = 14

/** A local day as the daemon spells it: "2026-10-10". */
export function localDay(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** The days the chart names: the last CHART_DAYS local days ending on now's, oldest first. */
export function chartDays(now: number): string[] {
  const out: string[] = []
  for (let back = CHART_DAYS - 1; back >= 0; back--) {
    const d = new Date(now)
    d.setDate(d.getDate() - back)
    out.push(localDay(d))
  }
  return out
}

/** The most days the chart draws for a long range. */
export const MAX_CHART_DAYS = 60

/** The chart's days for a range: its own days, at least CHART_DAYS and at most MAX_CHART_DAYS, ending on its last day, oldest first. */
export function rangeChartDays(from: string, to: string): string[] {
  const end = new Date(`${to}T12:00:00`)
  const start = new Date(`${from}T12:00:00`)
  const span = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1
  const n = Math.min(MAX_CHART_DAYS, Math.max(CHART_DAYS, Number.isFinite(span) ? span : CHART_DAYS))
  const out: string[] = []
  for (let back = n - 1; back >= 0; back--) {
    const d = new Date(end)
    d.setDate(d.getDate() - back)
    out.push(localDay(d))
  }
  return out
}

/** The first and last day a usage read covers: the chart's window ending on now's local day. */
export function usageWindow(now: number): { from: string; to: string } {
  const days = chartDays(now)
  return { from: days[0] as string, to: days[days.length - 1] as string }
}

/** How far through the day's budget today's spend is; null when there is no cap. */
export function budgetUse(spend: SpendToday): { fraction: number; percent: number; near: boolean; over: boolean } | null {
  if (!(spend.budget > 0)) return null
  const fraction = spend.usd / spend.budget
  return { fraction, percent: Math.round(fraction * 100), near: fraction >= 0.8 && fraction < 1, over: fraction >= 1 }
}

/** The hero's words under the figure: "of $25.00 today", and "near budget" past 80% of the cap. */
export function heroSub(spend: SpendToday): { of: string; note: string } {
  const of = spend.budget > 0 ? `of ${usd(spend.budget)} today` : `${usd(spend.usd) || '$0.00'} today`
  const use = budgetUse(spend)
  if (use?.over) return { of, note: 'over budget' }
  if (use?.near) return { of, note: 'near budget' }
  return { of, note: '' }
}

/** One card row: a label, its spend, and the full label for the hover when shortened. */
export interface CardRow {
  key: string
  label: string
  title?: string
  usd: number
  /** The agent or agents that ran the model, for a faint word after the label. */
  agent?: string
  /** Agent runs and front desk turns the row counts; absent when the card does not say. */
  runs?: number
  turns?: number
}

/** The card rows, biggest spend first: ties keep the daemon's order. */
export function cardRows(rows: CardRow[]): CardRow[] {
  return [...rows].sort((a, b) => b.usd - a.usd)
}

/** By agent: the 14 days' usage grouped by agent, biggest spend first. */
export function agentRows(stats: UsageStats | null): CardRow[] {
  const by = new Map<string, number>()
  for (const g of [...(stats?.by_model ?? []), ...(stats?.desk_by_model ?? [])]) {
    const agent = g.agent || 'unknown'
    by.set(agent, (by.get(agent) ?? 0) + g.cost_usd)
  }
  return cardRows([...by].map(([agent, dollars]) => ({ key: agent, label: agent, usd: dollars })))
}

/** The model id shown: drop any provider prefix before the last '/'. */
export function shortModel(model: string): string {
  const i = model.lastIndexOf('/')
  return i >= 0 ? model.slice(i + 1) : model
}

/** By model: the 14 days' usage grouped by model, biggest spend first. The label is the
 * short id; the title is the full id for the hover. */
export function modelRows(stats: UsageStats | null): CardRow[] {
  const by = new Map<string, { usd: number; runs: number; turns: number; agents: Set<string> }>()
  const add = (g: UsageGroup, desk: boolean) => {
    const model = g.model || g.key
    const cur = by.get(model) ?? { usd: 0, runs: 0, turns: 0, agents: new Set<string>() }
    cur.usd += g.cost_usd
    const n = typeof g.count === 'number' && Number.isFinite(g.count) ? g.count : 0
    if (desk) cur.turns += n
    else cur.runs += n
    if (g.agent) cur.agents.add(g.agent)
    by.set(model, cur)
  }
  for (const g of stats?.by_model ?? []) add(g, false)
  for (const g of stats?.desk_by_model ?? []) add(g, true)
  return cardRows(
    [...by].map(([model, v]) => {
      const short = shortModel(model)
      return {
        key: model, label: short, title: model === short ? undefined : model, usd: v.usd, runs: v.runs, turns: v.turns,
        ...(v.agents.size ? { agent: [...v.agents].join(', ') } : {}),
      }
    }),
  )
}

/** By project: the 14 days' lanes attributed through their repos, biggest spend first. A
 * repo the configuration puts in no project counts as Unassigned; a record with no lane
 * keeps its own name. */
export function projectRows(stats: UsageStats | null, ofRepo: (repo: string) => string): CardRow[] {
  const by = new Map<string, number>()
  for (const g of stats?.by_lane ?? []) {
    const label = g.repo ? ofRepo(g.repo) || 'Unassigned' : g.key
    by.set(label, (by.get(label) ?? 0) + g.cost_usd)
  }
  return cardRows([...by].map(([label, dollars]) => ({ key: label, label, usd: dollars })))
}

/** Which project's group a repo sits in: the name, or '' for Unassigned. */
export function projectOfRepo(projects: ProjectSummary[]): (repo: string) => string {
  const by = new Map<string, string>()
  for (const p of projects) {
    if (p.unassigned) continue
    for (const r of p.repos) by.set(r, p.name)
  }
  return (repo) => by.get(repo) ?? ''
}

/** A project's budget cap: its amount, or null when it has none. */
export function projectCap(projects: ProjectSummary[], name: string): number | null {
  const p = projects.find((q) => !q.unassigned && q.name === name)
  if (!p?.budget || !(p.budget.amount > 0)) return null
  return p.budget.amount
}

/** One day of the 14-day chart: its spend against the day's budget cap. */
export interface ChartDay {
  day: string
  usd: number
  /** Lichen under 80% of the cap, lantern at 80% and past it, broken at the cap. */
  state: 'over' | 'near' | 'under'
}

/** The chart's bars from each day's total and the daily cap, and the scale they share:
 * far enough for the biggest bar and the cap. Without a cap every bar is lichen. */
export function chartModel(days: string[], totals: Record<string, number>, cap: number): { bars: ChartDay[]; max: number } {
  const bars: ChartDay[] = days.map((day) => {
    const dollars = totals[day] ?? 0
    const state = cap > 0 && dollars >= cap ? 'over' : cap > 0 && dollars >= 0.8 * cap ? 'near' : 'under'
    return { day, usd: dollars, state }
  })
  const max = Math.max(cap, ...Object.values(totals), 0)
  return { bars, max }
}

/** A run's spend, in dollars. */
export function runSpend(r: RunRecord): number {
  return r.cost_usd ?? 0
}

/** The runs that started within a range's local days, first and last inclusive. */
export function runsInRange(runs: RunRecord[], from: string, to: string): RunRecord[] {
  const start = new Date(`${from}T00:00:00`).getTime()
  const end = new Date(`${to}T00:00:00`)
  end.setDate(end.getDate() + 1)
  return runs.filter((r) => {
    const t = Date.parse(r.started)
    return t >= start && t < end.getTime()
  })
}

/** The most expensive runs, most expensive first, cut to `limit`. */
export function costliestRuns(runs: RunRecord[], limit = 5): RunRecord[] {
  return [...runs].sort((a, b) => runSpend(b) - runSpend(a) || b.id - a.id).slice(0, limit)
}
