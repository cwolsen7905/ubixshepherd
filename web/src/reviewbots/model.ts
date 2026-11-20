import type { ReviewBot, ReviewBotCounts } from '../api/types'
import { usd } from '../format'

export const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

/** Spend for a cell: dollars, with four places under a cent where they matter ("$0.0022"). */
export function spend(n: number, stamped: number): string {
  if (stamped === 0) return 'not stamped'
  if (n > 0 && n < 0.1) return `$${n.toFixed(4)}`
  return usd(n)
}

/** The counts a table shows, in column order. */
export const COLUMNS: { key: keyof ReviewBotCounts; label: string; title: string }[] = [
  { key: 'jobs', label: 'Jobs', title: 'Merge requests the bot worked on in the window' },
  { key: 'proposals', label: 'Proposals', title: 'Merge requests whose latest status is proposed' },
  { key: 'dismissals', label: 'Dismissals', title: 'Threads the bot opened that a person resolved' },
  { key: 'needs_you', label: 'Needs you', title: 'Merge requests whose latest status is needs you' },
  { key: 'fixes', label: 'Fixes pushed', title: 'Merge requests whose latest status is pushed' },
]

/** The page's header summary: bots and what waits on the person across all of them. */
export function summary(bots: ReviewBot[]): string {
  const waiting = bots.reduce((n, b) => n + b.total.needs_you, 0)
  return `${plural(bots.length, 'bot')} · ${waiting} needing you`
}

/** The line under a tab: who the bot is, how far back, and how its labels begin. */
export function botMeta(b: ReviewBot): string {
  return `${b.user} · labels ${b.label_prefix}… · last ${plural(b.window_days, 'day')}`
}
