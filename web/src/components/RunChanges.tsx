import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { RunView } from '../api/types'
import { Changes } from '../components/Changes'

/**
 * The run's own changes: the lane's Changes view narrowed to the run's start_sha..end_sha.
 * Read when its tab is shown, since each read runs git; whether the run is the lane's latest
 * (the only one that shows the worktree's uncommitted work) is read then too.
 */
export function RunChanges({ run, now }: { run: RunView; now: number }) {
  const laneId = run.lane_id
  const runId = run.id
  const key = `${laneId}/${runId}`
  // The answer is kept with the run it was read for, so a different run reads as unread
  // until its own answer arrives.
  const [read, setRead] = useState<{ key: string; latest: boolean } | null>(null)
  const latest = read?.key === key ? read.latest : null
  useEffect(() => {
    let current = true
    api.runs(laneId, 1).then(
      (rs) => current && setRead({ key, latest: Array.isArray(rs) && rs[0]?.id === runId }),
      () => current && setRead({ key, latest: false }),
    )
    return () => {
      current = false
    }
  }, [laneId, runId, key])
  return (
    <div className="run-changes">
      {latest === null && <p className="faint">Reading…</p>}
      {latest !== null && (
        <Changes
          laneId={laneId}
          worktree={run.worktree}
          editorUrl={null}
          refreshKey={0}
          now={now}
          run={{ id: runId, latest, outside: run.outside }}
        />
      )}
    </div>
  )
}
