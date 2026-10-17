import { useEffect, useState } from 'react'
import { laneArtifacts } from '../api/artifacts'
import { laneChanges } from '../api/client'

/** What a lane's Changes and Artifacts tabs would list; each is unset until read, or when the read fails. */
export type LaneCounts = { changes?: number; artifacts?: number }

/** Reads the two counts for a tab strip, again when `refreshKey` moves (the feed advanced). */
export function useLaneCounts(laneId: number, refreshKey: number): LaneCounts {
  const [counts, setCounts] = useState<{ laneId: number } & LaneCounts>({ laneId })
  useEffect(() => {
    let current = true
    const put = (c: LaneCounts) => current && setCounts((prev) => ({ ...(prev.laneId === laneId ? prev : {}), laneId, ...c }))
    laneChanges.diff(laneId).then(
      (d) => put({ changes: d.committed.length + d.uncommitted.length + d.untracked.length + (d.files_more ?? 0) }),
      () => undefined,
    )
    laneArtifacts(laneId).then(
      (a) => put({ artifacts: a.artifacts.length }),
      () => undefined,
    )
    return () => {
      current = false
    }
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
