import { useEffect, useState } from 'react'
import { laneChanges } from '../api/client'
import type { LaneDiff, LaneView, RunView } from '../api/types'
import { ChangesList } from './ChangesList'
import { LaneFacts } from './LaneDetailFacts'
import type { LaneApprovalState } from './LaneDetailApprove'
import '../styles/changes.css'

/**
 * The page's Changes tab: the file lists on the left, the facts rail on the right. What the
 * panel's tab shows above the list (the summary line, the commits, "Read hh:mm" and Refresh)
 * moves into the rail, and the lane's Approve is the header's. The read is taken when the tab
 * opens, when `refreshKey` moves and on Refresh; it is not polled, since each read runs git.
 */
export function LaneChangesTab({ lane, runs, latest, now, editorUrl, refreshKey, approval }: {
  lane: LaneView
  runs: RunView[]
  latest?: RunView
  now: number
  editorUrl: string | null
  refreshKey: number
  approval: LaneApprovalState
}) {
  const [read, setRead] = useState<LaneDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [manual, setManual] = useState(0)
  const request = `${lane.id}:${refreshKey}:${manual}`
  const [answered, setAnswered] = useState('')
  useEffect(() => {
    let current = true
    laneChanges.diff(lane.id).then(
      (d) => {
        if (!current) return
        setRead(d)
        setError(null)
      },
      (e: unknown) => current && setError(e instanceof Error ? e.message : String(e)),
    ).finally(() => current && setAnswered(request))
    return () => {
      current = false
    }
  }, [lane.id, request])
  return (
    <div className="lane-overview-page lane-changes-page">
      <div className="lane-overview-main">
        <div aria-label="Changes in the lane" className="changes">
          {error && <p className="tone-bad" role="alert">Could not read the lane's changes: {error}</p>}
          {!read && !error && <p className="faint">Reading…</p>}
          {read && <ChangesList read={read} worktree={lane.worktree} editorUrl={editorUrl} variant="diff" lead={false} />}
        </div>
      </div>
      <aside className="lane-facts-rail" aria-label="Lane facts">
        <LaneFacts
          lane={lane}
          runs={runs}
          latest={latest}
          now={now}
          editorUrl={editorUrl}
          page
          approval={approval}
          changes={{ read, reading: answered !== request, onRefresh: () => setManual((n) => n + 1) }}
        />
      </aside>
    </div>
  )
}
