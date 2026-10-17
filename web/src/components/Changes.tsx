import { useEffect, useState } from 'react'
import { laneChanges } from '../api/client'
import type { LaneDiff } from '../api/types'
import { clockTime } from '../format'
import { ChangesList, type RunRange } from '../ui/ChangesList'
import '../styles/changes.css'

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/**
 * The lane's Changes tab: what the lane holds against its base. The read is taken when the
 * tab opens, when `refreshKey` moves (the feed advanced) and on Refresh; it is not polled,
 * since each read runs git. A file's patch is read only when it is opened.
 */
export function Changes({ laneId, worktree, editorUrl, refreshKey, now, run }: {
  laneId: number
  worktree: string
  /** Narrow the read to one run's range (the run page). */
  run?: RunRange
  /** The editor link template; "" or null hides the links. */
  editorUrl: string | null
  refreshKey: number
  now: number
}) {
  const [read, setRead] = useState<LaneDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [manual, setManual] = useState(0)
  const runId = run?.id
  const request = `${laneId}:${runId ?? ''}:${refreshKey}:${manual}`
  const [answered, setAnswered] = useState('')
  const busy = answered !== request

  useEffect(() => {
    let current = true
    laneChanges.diff(laneId, undefined, runId).then(
      (d) => {
        if (!current) return
        setRead(d)
        setError(null)
      },
      (e: unknown) => current && setError(message(e)),
    ).finally(() => current && setAnswered(request))
    return () => {
      current = false
    }
  }, [laneId, runId, request])

  return (
    <div
      aria-label={run ? `Changes in run ${run.id}` : 'Changes in the lane'}
      className="changes"
    >
      <div className="changes-bar">
        <span className="faint">
          {read ? <>Read <time dateTime={read.read_at} title={read.read_at}>{clockTime(read.read_at, now)}</time></> : busy ? 'Reading…' : ''}
        </span>
        <button type="button" className="link-button" disabled={busy} onClick={() => setManual((n) => n + 1)}>
          {busy ? 'Reading…' : 'Refresh'}
        </button>
      </div>
      {error && <p className="tone-bad" role="alert">Could not read the lane's changes: {error}</p>}
      {read && <ChangesList read={read} worktree={worktree} editorUrl={editorUrl} run={run} variant="diff" />}
    </div>
  )
}

export { lineClass, Patch } from '../ui/ChangesList'
