import { useEffect, useMemo, useRef, useState } from 'react'
import type { DeskQuestion } from '../api/types'
import { deskQuestions } from '../api/client'
import { DeskThread } from '../state/desk'
import { Button, EmptyState, IdChip, ListRow } from '../ui'

/**
 * Reply here and Skip under a desk message that asked the person something. Reply opens an
 * input right here; sending it posts a normal turn linked to the question, and the daemon clears it
 * whatever the words say. Skip, Mark answered or the desk's question_answer also clear it.
 */
function QuestionActions({
  q, thread, topicId = null, onCleared, onReplied,
}: {
  q: DeskQuestion
  thread: DeskThread
  topicId?: number | null
  /** Called once the question is no longer open (answered or skipped). */
  onCleared: () => void
  /** Called after a reply was accepted: the daemon has cleared the question. */
  onReplied?: () => void
}) {
  const [replying, setReplying] = useState(false)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  useEffect(() => {
    if (replying) input.current?.focus()
  }, [replying])

  const send = async () => {
    if (busy || !text.trim()) return
    setBusy(true)
    setProblem(null)
    const ok = await thread.send(text, topicId, q.id)
    setBusy(false)
    if (ok) {
      setText('')
      setReplying(false)
      onReplied?.()
    } else {
      setProblem('Not sent. The desk may be busy; try again.')
    }
  }
  const close = async (how: 'answer' | 'skip') => {
    setBusy(true)
    setProblem(null)
    try {
      await deskQuestions[how](q.id)
      onCleared()
    } catch (err) {
      setProblem(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="sh-question" role="group" aria-label={`Question ${q.id} is waiting on you`}>
      <span className="sh-question-flag">? Waiting on you</span>
      <IdChip kind="question" id={q.id} />
      {!replying && (
        <>
          <Button size="small" onClick={() => setReplying(true)} disabled={busy}>
            Reply here
          </Button>
          <Button size="small" onClick={() => void close('skip')} disabled={busy}>
            Skip
          </Button>
          <Button size="small" onClick={() => void close('answer')} disabled={busy}>
            Mark answered
          </Button>
        </>
      )}
      {replying && (
        <form
          className="sh-question-reply"
          onSubmit={(e) => {
            e.preventDefault()
            void send()
          }}
        >
          <label htmlFor={`question-reply-${q.id}`} className="visually-hidden">
            Your reply to question {q.id}
          </label>
          <textarea
            id={`question-reply-${q.id}`}
            ref={input}
            rows={2}
            value={text}
            readOnly={busy}
            placeholder="Reply to this question. Enter sends, Shift+Enter adds a line."
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                void send()
              }
              if (e.key === 'Escape') setReplying(false)
            }}
          />
          <Button type="submit" variant="primary" disabled={busy || !text.trim()}>
            {busy ? 'Sending…' : 'Send reply'}
          </Button>
          <Button onClick={() => setReplying(false)} disabled={busy}>
            Cancel
          </Button>
        </form>
      )}
      {problem && (
        <p className="sh-question-problem" role="alert">
          {problem}
        </p>
      )}
    </div>
  )
}

/**
 * The open questions in one list: the lit one is selected, a click lights a row, and Mark
 * answered or Skip on a question drops it from the list at once and lights the next; the last
 * one leaves the empty state. A reply the desk accepts clears its question the same way.
 */
export function QuestionList({
  questions, selected = null, thread, topicId = null, onCleared, onSelected,
}: {
  /** The questions as read from the daemon; the open ones list. */
  questions: DeskQuestion[]
  /** The question to light; the first open one when null or not in the list. */
  selected?: number | null
  thread: DeskThread
  topicId?: number | null
  /** Called with the question's id once it is cleared, so the parent can re-read. */
  onCleared?: (id: number) => void
  /** Called when the person changes the lit question, with the new id or null when none is left. */
  onSelected?: (id: number | null) => void
}) {
  // The ones the person just cleared: out of the list at once, until the daemon's read agrees.
  const [gone, setGone] = useState<ReadonlySet<number>>(() => new Set())
  const shown = useMemo(
    () => questions.filter((q) => q.state === 'open' && !gone.has(q.id)),
    [questions, gone],
  )
  // The row the person lit by clicking; the prop's question and the first open one fill in
  // for it, so the light follows the list as the reads and the clears change it.
  const [picked, setPicked] = useState<number | null>(null)
  // A change of the prop from outside (not this list's own pick) takes the light back.
  const [prevSelected, setPrevSelected] = useState(selected)
  if (selected !== prevSelected) {
    setPrevSelected(selected)
    setPicked(null)
  }
  const inShown = (id: number | null) => id !== null && shown.some((q) => q.id === id)
  const lit = inShown(picked) ? picked : inShown(selected) ? selected : shown[0]?.id ?? null
  const pick = (id: number | null) => {
    if (id === lit) return
    setPicked(id)
    onSelected?.(id)
  }

  // A cleared question leaves at once: the next one takes the light, or the empty state.
  const clear = (q: DeskQuestion) => {
    setGone((g) => {
      const live = new Set(questions.map((x) => x.id))
      return new Set([...g].filter((id) => live.has(id))).add(q.id)
    })
    const rest = shown.filter((x) => x.id !== q.id)
    const after = shown.findIndex((x) => x.id === q.id)
    const next = rest.some((x) => x.id === lit) ? lit : rest[after]?.id ?? rest[0]?.id ?? null
    pick(next)
    onCleared?.(q.id)
  }

  if (shown.length === 0) {
    return <EmptyState title="No open questions." />
  }
  return (
    <ul className="sh-question-list" data-component="QuestionList">
      {shown.map((q) => (
        <li key={q.id}>
          <ListRow
            glyph="?"
            tone="warn"
            glyphLabel="question"
            title={q.text}
            titleAttr={q.text}
            selected={lit === q.id}
            onSelect={() => pick(q.id)}
          />
          <QuestionActions q={q} thread={thread} topicId={topicId} onCleared={() => clear(q)} onReplied={() => clear(q)} />
        </li>
      ))}
    </ul>
  )
}

export { QuestionActions }
