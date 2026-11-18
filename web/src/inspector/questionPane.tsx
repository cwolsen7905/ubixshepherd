import { useCallback, useEffect, useRef, useState } from 'react'
import { deskQuestions } from '../api/client'
import type { DeskQuestion } from '../api/types'
import { QuestionList } from '../chat/questions'
import { advanceFrom } from '../chat/strip'
import { waitingKey } from '../model/waitingStep'
import { useSnapshot } from '../state/context'
import { DeskThread } from '../state/desk'

const defaultMake = (w: number) => new DeskThread(w)

/**
 * A desk question in the inspector: the open questions as one list, the one asked for lit.
 * Mark answered or Skip drops it and lights the next; the last leaves the empty state. The
 * lit question is the list's own. When the last one is cleared the inspector moves on to the
 * next waiting item of any kind, as the other panes do.
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
  // A reply posts to the thread of its own question's workspace, one thread per workspace.
  const threads = useRef(new Map<number, DeskThread>())
  const threadFor = useCallback(
    (q: DeskQuestion) => {
      const ws = workspaces.length > 1 ? q.workspace_id : 0
      const have = threads.current.get(ws)
      if (have) return have
      const made = make(ws)
      threads.current.set(ws, made)
      return made
    },
    [make, workspaces.length],
  )
  // The questions cleared here, so the last one can hand the inspector on to the next waiting item.
  const cleared = useRef<Set<number>>(new Set())
  const onCleared = (clearedId: number) => {
    cleared.current.add(clearedId)
    read()
    if (open.every((x) => cleared.current.has(x.id))) {
      const key = waitingKey({ kind: 'question', action: 'reply', question_id: clearedId, title: '' })
      advanceFrom(snap.waiting?.items ?? [], key)
    }
  }
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
      <QuestionList questions={got} selected={id} thread={threadFor} onCleared={onCleared} />
    </div>
  )
}
