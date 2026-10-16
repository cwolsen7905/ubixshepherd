import { useEffect, useState } from 'react'
import { artifactContentPath, artifactText, type ArtifactInfo, type ArtifactText } from '../api/artifacts'
import { ArtifactMarkdown } from './ArtifactMarkdown'
import { EmptyState, Skeleton } from '../ui'

/** The human size of a file. */
export function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

/**
 * One artifact, rendered: markdown through the shared renderer, text and code as preformatted
 * text, an image as an `<img>` (never inline SVG). Content is untrusted and never becomes
 * markup. Reusable: it needs only a lane id and the listed artifact.
 */
export function ArtifactViewer({ laneId, artifact, refreshKey }: { laneId: number; artifact: ArtifactInfo; refreshKey?: number }) {
  const { path, kind, readable } = artifact
  const textual = readable && (kind === 'markdown' || kind === 'text' || kind === 'code')
  const [read, setRead] = useState<{ key: string; value?: ArtifactText; error?: string } | null>(null)
  const key = `${laneId}:${path}:${refreshKey ?? 0}`
  useEffect(() => {
    if (!textual) return
    let current = true
    artifactText(laneId, path).then(
      (value) => current && setRead({ key, value }),
      (e: unknown) => current && setRead({ key, error: e instanceof Error ? e.message : String(e) }),
    )
    return () => {
      current = false
    }
  }, [laneId, path, textual, key])

  const head = (
    <p className="artifact-head">
      <code>{path}</code> <span className="faint">{kind} · {bytes(artifact.size)}</span>
    </p>
  )
  if (!readable) {
    return (
      <div className="artifact-view">
        {head}
        <EmptyState title="Not readable here">
          This file is {bytes(artifact.size)}: it is over the size cap, a link, or no longer there. Open it in the lane's worktree.
        </EmptyState>
      </div>
    )
  }
  if (kind === 'image') {
    return (
      <div className="artifact-view">
        {head}
        <img className="artifact-image" src={artifactContentPath(laneId, path)} alt={path} />
      </div>
    )
  }
  if (!textual) {
    return (
      <div className="artifact-view">
        {head}
        <EmptyState title="No preview">There is no preview for this kind of file.</EmptyState>
      </div>
    )
  }
  const mine = read?.key === key ? read : null
  return (
    <div className="artifact-view">
      {head}
      {!mine && <Skeleton lines={3} />}
      {mine?.error && <EmptyState title="Could not read the file">{mine.error}</EmptyState>}
      {mine?.value && (
        <>
          {mine.value.redacted > 0 && (
            <p className="faint">{mine.value.redacted} secret-like {mine.value.redacted === 1 ? 'span was' : 'spans were'} masked.</p>
          )}
          {kind === 'markdown' ? <ArtifactMarkdown text={mine.value.text} laneId={laneId} path={path} /> : <pre className="artifact-text">{mine.value.text}</pre>}
        </>
      )}
    </div>
  )
}
