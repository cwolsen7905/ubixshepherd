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

/** A desk question in the inspector: what the desk asked, with Reply and Skip. */
export function QuestionPane({ id, make = (w: number) => new DeskThread(w) }: { id: number; make?: (workspaceId: number) => DeskThread }) {
  const snap = useSnapshot()
  const [q, setQ] = useState<DeskQuestion | null | undefined>(undefined)
  const read = useCallback(() => {
    deskQuestions.list('open').then(
      (qs) => setQ(qs.find((x) => x.id === id) ?? null),
      () => {},
    )
  }, [id])
  useEffect(() => {
    const first = window.setTimeout(read, 0)
    return () => window.clearTimeout(first)
  }, [read])
  const workspaces = snap.status?.workspaces ?? []
  const thread = useMemo(() => (q ? make(workspaces.length > 1 ? q.workspace_id : 0) : null), [q, workspaces.length, make])
  if (q === undefined) return <p className="inspector-body faint">Reading the question…</p>
  if (q === null || !thread) {
    return (
      <p className="inspector-body" role="status">
        Question {id} is not waiting on you any more. <a href={href.chat()}>Back to the conversation</a>.
      </p>
    )
  }
  const key = waitingKey({ kind: 'question', action: 'reply', question_id: id, title: '' })
  return (
    <div className="inspector-body">
      <p className="faint">The front desk asked, in turn {q.turn}:</p>
      <Markdown text={q.text} />
      <QuestionActions q={q} thread={thread} topicId={q.topic_id ?? null} onCleared={() => advanceFrom(snap.waiting?.items ?? [], key)} />
    </div>
  )
}
