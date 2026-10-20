import { useEffect, useState } from 'react'
import { laneArtifacts, type LaneArtifacts as Listing } from '../api/artifacts'
import { EmptyState, ErrorState, LoadingRows } from '../ui'
import { ArtifactViewer, bytes } from './ArtifactViewer'

/** A lane's artifacts: the list, and the chosen one below it. */
export function LaneArtifactsPanel({ laneId, refreshKey }: { laneId: number; refreshKey?: number }) {
  const [list, setList] = useState<Listing | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [chosen, setChosen] = useState<string | null>(null)
  useEffect(() => {
    let current = true
    laneArtifacts(laneId).then(
      (l) => {
        if (!current) return
        setList(l)
        setError(null)
      },
      (e: unknown) => current && setError(e instanceof Error ? e.message : String(e)),
    )
    return () => {
      current = false
    }
  }, [laneId, refreshKey])

  if (error) return <ErrorState what="The artifacts" error={error} />
  if (!list) return <LoadingRows />
  if (list.artifacts.length === 0) {
    return (
      <EmptyState title="No artifacts in this lane yet">
        {list.globs.length > 0 ? <>Looking for <code>{list.globs.join(', ')}</code>.</> : 'This repo sets no artifact globs.'}
      </EmptyState>
    )
  }
  const artifact = list.artifacts.find((a) => a.path === chosen)
  return (
    <div className="artifacts">
      <ul className="artifact-list">
        {list.artifacts.map((a) => (
          <li key={a.path}>
            <button type="button" className="link-button" aria-current={a.path === chosen} onClick={() => setChosen(a.path)}>
              {a.path}
            </button>{' '}
            <span className="faint">{a.kind} · {a.source} · {bytes(a.size)}</span>
          </li>
        ))}
      </ul>
      {list.truncated && <p className="faint">The list was cut short.</p>}
      {artifact ? <ArtifactViewer laneId={laneId} artifact={artifact} refreshKey={refreshKey} /> : <p className="faint">Choose a file to read it.</p>}
    </div>
  )
}
