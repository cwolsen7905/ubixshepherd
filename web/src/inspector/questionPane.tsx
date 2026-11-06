import { useCallback, useEffect, useMemo, useState } from 'react'
import { deskQuestions } from '../api/client'
import type { DeskQuestion } from '../api/types'
import { QuestionList } from '../chat/questions'
import { useSnapshot } from '../state/context'
import { DeskThread } from '../state/desk'

const defaultMake = (w: number) => new DeskThread(w)

/**
 * A desk question in the inspector: the open questions as one list, the one asked for lit.
 * Mark answered or Skip drops it and lights the next; the last leaves the empty state. The
 * lit question is the list's own, so answering never re-reads the pane from scratch.
 */
export function QuestionPane({ id, make = defaultMake }: { id: number; make?: (workspaceId: number) => DeskThread }) {
  const snap = useSnapshot()
  const [got, setQ] = useState<DeskQuestion[] | undefined>(undefined)
  const [failed, setFailed] = useState(false)
  const read = useCallback(() => {
    setFailed(false)
    deskQuestions.list('open').then(setQ, () => setFailed(true))
  }, [])
  useEffect(() => {
    const first = window.setTimeout(read, 0)
    return () => window.clearTimeout(first)
  }, [read])
  const workspaces = snap.status?.workspaces ?? []
  const open = got?.filter((x) => x.state === 'open') ?? []
  // The thread a reply posts to: the workspace of the question asked for, or the first open one.
  const wsId = workspaces.length > 1 ? (open.find((x) => x.id === id)?.workspace_id ?? open[0]?.workspace_id ?? 0) : 0
  const thread = useMemo(() => make(wsId), [wsId, make])
  if (failed && got === undefined) {
    return (
      <p className="inspector-body tone-bad" role="alert">
        Could not read the questions.{' '}
        <button type="button" className="link-button" onClick={read}>
          Try again
        </button>
      </p>
    )
  }
  if (got === undefined) return <p className="inspector-body faint">Reading the questions…</p>
  return (
    <div className="inspector-body question-pane">
      <QuestionList questions={got} selected={id} thread={thread} onCleared={read} />
    </div>
  )
}
