import { useEffect, useRef, useState } from 'react'
import { laneArtifacts } from '../api/artifacts'
import { laneChanges } from '../api/client'

/** What a lane's Changes and Artifacts tabs would list; each is unset until read, or when the read fails. */
export type LaneCounts = { changes?: number; artifacts?: number }

/** The least time between two reads for one lane: the diff runs git in the worktree. */
const MIN_REREAD = 15_000

/**
 * Reads the two counts for a tab strip: when the lane changes, and again when `refreshKey` moves
 * (the feed advanced), but no sooner than MIN_REREAD after the last read: a burst of events gets
 * one trailing read when the window ends, so a busy feed does not run git on every event.
 */
export function useLaneCounts(laneId: number, refreshKey: number): LaneCounts {
  const [counts, setCounts] = useState<{ laneId: number } & LaneCounts>({ laneId })
  const last = useRef({ laneId: 0, at: 0 })
  useEffect(() => {
    const read = () => {
      last.current = { laneId, at: Date.now() }
      // A late answer for a lane we have left is dropped.
      const put = (c: LaneCounts) => last.current.laneId === laneId && setCounts((prev) => ({ ...(prev.laneId === laneId ? prev : {}), laneId, ...c }))
      laneChanges.diff(laneId).then(
        (d) => put({ changes: d.committed.length + d.uncommitted.length + d.untracked.length + (d.files_more ?? 0) }),
        () => undefined,
      )
      laneArtifacts(laneId).then(
        (a) => put({ artifacts: a.artifacts.length }),
        () => undefined,
      )
    }
    const wait = last.current.laneId === laneId ? MIN_REREAD - (Date.now() - last.current.at) : 0
    if (wait <= 0) return read()
    // Too soon: one trailing read when the window ends, so the last state of a burst is read.
    // A newer event replaces the timer; leaving the lane or unmounting clears it.
    const t = window.setTimeout(read, wait)
    return () => window.clearTimeout(t)
  }, [laneId, refreshKey])
  return counts.laneId === laneId ? counts : {}
}

/** A tab label with its count beside it, once the count is known. */
export function tabLabel(name: string, n: number | undefined) {
  return n === undefined ? name : (
    <>
      {name} <span className="tab-count">{n}</span>
    </>
  )
}
