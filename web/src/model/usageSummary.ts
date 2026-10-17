// The usage view's four summary cards, from the range's totals. Pure functions, so a
// missing number prints nothing rather than NaN.
import type { UsageStats, UsageTotals } from '../api/types'
import { runCost, tokens, usd } from '../format'

/** A total's field, 0 when the daemon left it out or it is not a number. */
const num = (v: number | undefined): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0)

/** A count or token figure: empty when the daemon left it out. */
export function figure(v: number | undefined, fmt: (n: number) => string = String): string {
  return typeof v === 'number' && Number.isFinite(v) ? fmt(v) : ''
}

/** The money of a set of records: dollars, with Copilot's credits beside them; "$0.00" for none. */
export function totalCost(t: Pick<UsageTotals, 'cost_usd' | 'credits' | 'estimated_usd'> | undefined): string {
  return (t && runCost(t)) || usd(0)
}

export interface SummaryCard {
  label: string
  value: string
  sub: string
}

/** Total cost (with credits), Desk vs agents, Runs and turns, Over 200k. */
export function summaryCards(s: UsageStats): SummaryCard[] {
  const total = num(s.total?.cost_usd)
  const desk = num(s.desk?.cost_usd)
  const runs = num(s.runs?.cost_usd)
  const over = num(s.runs?.over_200k) + num(s.desk?.over_200k)
  const measured = num(s.runs?.measured) + num(s.desk?.measured)
  const peak = Math.max(num(s.runs?.peak_context), num(s.desk?.peak_context))
  const estimated = num(s.total?.estimated_usd)
  const share = total > 0 ? Math.round((desk / total) * 100) : 0
  return [
    {
      label: 'Total cost',
      value: totalCost(s.total),
      sub: `${num(s.total?.count)} records${estimated > 0 ? `, of which ${usd(estimated)} is an estimate` : ''}`,
    },
    {
      label: 'Desk vs agents',
      value: `${usd(desk) || '$0.00'} / ${usd(runs) || '$0.00'}`,
      sub: total > 0 ? `the desk is ${share}% of dollars` : 'front desk / agent runs',
    },
    {
      label: 'Runs and turns',
      value: `${num(s.runs?.count)} / ${num(s.desk?.count)}`,
      sub: `agent runs / desk turns, ${measured} measured`,
    },
    {
      label: 'Over 200k',
      value: String(over),
      sub: `of ${measured} measured${peak > 0 ? `; peak ${tokens(peak)}` : ''}${over === 0 && measured > 0 ? ', a 200k window held all of it' : ''}`,
    },
  ]
}
