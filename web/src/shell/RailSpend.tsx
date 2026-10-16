import type { SpendToday } from '../api/types'
import { usd } from '../format'

/** Today's spend as one compact line, with a thin bar against the day's budget when there is one. The full words live in the status popover. */
export function RailSpend({ spend }: { spend: SpendToday }) {
  const amount = usd(spend.usd) || '$0.00'
  const full = spend.budget > 0 ? `${amount} of ${usd(spend.budget)} today` : `${amount} today`
  const fraction = spend.budget > 0 ? spend.usd / spend.budget : 0
  const tone = fraction >= 1 ? 'bad' : fraction >= 0.8 ? 'warn' : 'ok'
  return (
    <span className="rail-spend" title={full}>
      {spend.budget > 0 && (
        <span
          className={`meter tone-${tone}`}
          role="meter"
          aria-label="Spent today"
          aria-valuemin={0}
          aria-valuemax={spend.budget}
          aria-valuenow={spend.usd}
          aria-valuetext={full}
        >
          <span className="meter-fill" style={{ width: `${Math.min(100, fraction * 100)}%` }} />
        </span>
      )}
      <span className="rail-spend-text">{spend.budget > 0 ? `${amount} / ${usd(spend.budget)}` : amount}</span>
    </span>
  )
}

/** The popover's full words for the same spend. */
export function spendWords(spend: SpendToday): string {
  const amount = usd(spend.usd) || '$0.00'
  return spend.budget > 0 ? `${amount} of ${usd(spend.budget)}` : `${amount}, no daily budget`
}
