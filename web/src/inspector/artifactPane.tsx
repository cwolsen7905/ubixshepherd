import { StoredArtifactView } from '../artifacts/StoredArtifact'
import { useSnapshot } from '../state/context'

/** The inspector's body for `open=artifact:N`: the file rendered, with its versions and choices. */
export function ArtifactInspector({ id }: { id: number }) {
  // A feed event for the artifact's lane (a run's end, a new version) reads it again.
  const refreshKey = useSnapshot().feed.at(-1)?.id ?? 0
  return (
    <div className="inspector-body artifact-inspector">
      <StoredArtifactView id={id} refreshKey={refreshKey} />
    </div>
  )
}
