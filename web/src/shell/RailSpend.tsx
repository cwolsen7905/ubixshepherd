import type { SpendToday } from '../api/types'
import { usd } from '../format'
import { Meter } from '../ui'

/** The sidebar's spend: "$20.40 of $25.00 today" over a meter against the day's budget when there is one. */
export function RailSpend({ spend }: { spend: SpendToday }) {
  const amount = usd(spend.usd) || '$0.00'
  const full = spend.budget > 0 ? `${amount} of ${usd(spend.budget)} today` : `${amount} today`
  return (
    <div className="sh-spend" data-component="Spend" title={full}>
      <span>{full}</span>
      {spend.budget > 0 && <Meter value={spend.usd} max={spend.budget} label="Spent today" valueText={full} />}
    </div>
  )
}

/** The popover's full words for the same spend. */
export function spendWords(spend: SpendToday): string {
  const amount = usd(spend.usd) || '$0.00'
  return spend.budget > 0 ? `${amount} of ${usd(spend.budget)}` : `${amount}, no daily budget`
}
