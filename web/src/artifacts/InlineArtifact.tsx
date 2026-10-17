import { useEffect, useState } from 'react'
import type { ArtifactLink } from '../api/types'
import { ArtifactView } from '../ui/ArtifactView'
import type { DecisionPlan } from '../ui/DecisionView'
import { EmptyState, Skeleton } from '../ui'
import { ArtifactMarkdown } from './ArtifactMarkdown'
import { bytes } from './ArtifactViewer'
import { intentTag, storedArtifacts, storedContentPath, storedText, type StoredArtifact } from './api'

const textual = (a: StoredArtifact) => a.kind === 'markdown' || a.kind === 'text' || a.kind === 'code'

/** A stored artifact's content, drawn inline in another page: no bar, no actions. Never markup. */
export function InlineArtifact({ artifact: a }: { artifact: StoredArtifact }) {
  const wants = textual(a) && a.state !== 'discarded'
  const key = `${a.id}:${a.sha256 ?? ''}`
  const [read, setRead] = useState<{ key: string; text?: string; error?: string } | null>(null)
  useEffect(() => {
    if (!wants) return
    let current = true
    storedText(a.id).then(
      (t) => current && setRead({ key, text: t.text }),
      (e: unknown) => current && setRead({ key, error: e instanceof Error ? e.message : String(e) }),
    )
    return () => {
      current = false
    }
  }, [a.id, key, wants])
  let body
  const mine = read?.key === key ? read : null
  if (a.state === 'discarded') body = <EmptyState title="Discarded">This file was deleted at the person’s word.</EmptyState>
  else if (a.kind === 'image') body = <img src={storedContentPath(a.id)} alt={a.path} />
  else if (!wants) body = <EmptyState title="No preview">There is no preview for this kind of file.</EmptyState>
  else if (!mine) body = <Skeleton lines={3} />
  else if (mine.error) body = <EmptyState title="Could not read the file">{mine.error}</EmptyState>
  else if (a.kind === 'markdown') body = <ArtifactMarkdown text={mine.text ?? ''} laneId={a.lane_id} path={a.path} />
  else body = <pre>{mine.text}</pre>
  return <ArtifactView layout="inline">{body}</ArtifactView>
}

/**
 * The plan a decision links to, as the decision page draws it: the file's path, what Shepherd does
 * with it, run and size, and the content. Until the daemon has answered, the path alone.
 */
export function useDecisionPlan(link: ArtifactLink | undefined): DecisionPlan | undefined {
  const [got, setGot] = useState<{ id: number; a?: StoredArtifact } | null>(null)
  const id = link?.id
  useEffect(() => {
    if (id === undefined) return
    let current = true
    storedArtifacts.one(id).then(
      (a) => current && setGot({ id, a }),
      () => current && setGot({ id }),
    )
    return () => {
      current = false
    }
  }, [id])
  if (!link) return undefined
  const a = got?.id === link.id ? got.a : undefined
  if (!a) return { path: link.path, children: got?.id === link.id ? <p className="faint">Could not read this file.</p> : <Skeleton lines={3} /> }
  return {
    path: a.path,
    tag: intentTag(a),
    tagTone: a.intent === 'keep' ? 'local' : 'plain',
    meta: `run ${a.run_id} · ${bytes(a.size)}`,
    children: <InlineArtifact artifact={a} />,
  }
}
