import { usd } from '../format'
import { budgetUse, periodPhrase } from '../model/projects'
import type { ProjectBudget } from '../api/types'
import { Meter } from '../ui'

/** A project's spend against its budget: a bar and the words, never the colour alone. */
export function BudgetMeter({ budget, today }: { budget?: ProjectBudget; today: number }) {
  const use = budgetUse(budget)
  if (!budget) {
    return <span className="budget-none faint">{usd(today) || '$0.00'} today</span>
  }
  const phrase = periodPhrase(budget.period)
  if (!use) {
    return (
      <span className="budget">
        {usd(budget.spent) || '$0.00'} {phrase}
        <span className="faint"> · no cap</span>
      </span>
    )
  }
  const state = use.fraction >= 1 ? (budget.cap === 'hard' ? 'at its hard cap' : 'over its soft cap') : use.fraction >= 0.8 ? 'near its cap' : ''
  return (
    <span className="budget">
      <Meter value={budget.spent} max={budget.amount} label={`Spent ${phrase}`} valueText={`${usd(budget.spent) || '$0.00'} of ${usd(budget.amount)}`} />
      <span>
        {usd(budget.spent) || '$0.00'} of {usd(budget.amount)} {phrase}
      </span>
      {state && <span className={`tone-${use.tone}`}> · {state}</span>}
      {budget.period === 'month' && <span className="faint"> · {usd(today) || '$0.00'} today</span>}
    </span>
  )
}
