import { useState } from 'react'
import { age } from '../format'
import { useNow } from '../hooks'
import type { Status } from '../api/types'

/** A restart's wait, as GET /v1/status reports it while the daemon drains (internal/api Draining). */
export interface Drain {
  runs: number
  until: string
  drained: boolean
}

/** The drain a status carries, if any. */
export function drainOf(status: Status | null): Drain | null {
  return (status as (Status & { draining?: Drain }) | null)?.draining ?? null
}

/** "Restarting: draining 4 runs, 9m left". */
export function restartWords(d: Drain, now: number): string {
  if (d.drained) return 'Restarting'
  const left = Math.max(0, Date.parse(d.until) - now)
  const runs = `${d.runs} ${d.runs === 1 ? 'run' : 'runs'}`
  const time = left >= 60_000 ? `${Math.round(left / 60_000)}m` : age(left)
  return `Restarting: draining ${runs}, ${time} left`
}

/**
 * The sidebar line above the status line while the daemon drains for a restart: a muted ↻, the
 * words and a control-colour bar for how much of the wait has gone. Neutral, not lantern:
 * nothing waits on the person.
 */
export function RestartLine({ drain, now: nowProp }: { drain: Drain; now?: number }) {
  const ticking = useNow(1000)
  const now = nowProp ?? ticking
  const [first] = useState(() => Date.now())
  const until = Date.parse(drain.until)
  const total = until - first
  const gone = drain.drained ? 1 : total > 0 ? Math.min(1, Math.max(0, (now - first) / total)) : 0
  return (
    <div className="sh-restart" data-component="RestartLine" role="status">
      <div className="sh-restart-line">
        <span className="sh-glyph" aria-hidden="true">↻</span>
        <span>{restartWords(drain, now)}</span>
      </div>
      <div className="sh-meter" aria-hidden="true">
        <b style={{ width: `${gone * 100}%` }} />
      </div>
    </div>
  )
}
