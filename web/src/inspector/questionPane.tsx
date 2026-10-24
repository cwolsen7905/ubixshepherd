import { useCallback, useEffect, useMemo, useState } from 'react'
import { deskQuestions } from '../api/client'
import type { DeskQuestion } from '../api/types'
import { QuestionActions } from '../chat/questions'
import { advanceFrom } from '../chat/strip'
import { Markdown } from '../chat/md'
import { waitingKey } from '../model/waitingStep'
import { href } from '../router'
import { useSnapshot } from '../state/context'
import { DeskThread } from '../state/desk'
import { IdChip } from '../ui'

const defaultMake = (w: number) => new DeskThread(w)

/** A desk question in the inspector: what the desk asked, with Reply and Skip. */
export function QuestionPane({ id, make = defaultMake }: { id: number; make?: (workspaceId: number) => DeskThread }) {
  const snap = useSnapshot()
  const [got, setQ] = useState<DeskQuestion | null | undefined>(undefined)
  const q = got && got.id !== id ? undefined : got
  const [failed, setFailed] = useState(false)
  const read = useCallback(() => {
    setFailed(false)
    deskQuestions.list('open').then(
      (qs) => setQ(qs.find((x) => x.id === id) ?? null),
      () => setFailed(true),
    )
  }, [id])
  useEffect(() => {
    const first = window.setTimeout(read, 0)
    return () => window.clearTimeout(first)
  }, [read])
  const workspaces = snap.status?.workspaces ?? []
  // Never act on a question other than the one asked for, while a new read is in flight.
  const thread = useMemo(() => (q ? make(workspaces.length > 1 ? q.workspace_id : 0) : null), [q, workspaces.length, make])
  if (q === undefined && failed) {
    return (
      <p className="inspector-body tone-bad" role="alert">
        Could not read the question.{' '}
        <button type="button" className="link-button" onClick={read}>
          Try again
        </button>
      </p>
    )
  }
  if (q === undefined) return <p className="inspector-body faint">Reading the question…</p>
  if (q === null || !thread) {
    return (
      <p className="inspector-body" role="status">
        <IdChip kind="question" id={id} /> is not waiting on you any more. <a href={href.chat()}>Back to the conversation</a>.
      </p>
    )
  }
  const key = waitingKey({ kind: 'question', action: 'reply', question_id: id, title: '' })
  return (
    <div className="inspector-body question-pane">
      <p className="faint">The front desk asked, in turn {q.turn}:</p>
      <Markdown text={q.text} />
      <QuestionActions q={q} thread={thread} topicId={q.topic_id ?? null} onCleared={() => advanceFrom(snap.waiting?.items ?? [], key)} />
    </div>
  )
}
