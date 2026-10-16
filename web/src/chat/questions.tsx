import { useEffect, useRef, useState } from 'react'
import type { DeskQuestion } from '../api/types'
import { deskQuestions } from '../api/client'
import { DeskThread } from '../state/desk'

/**
 * Reply here and Skip under a desk message that asked the person something. Reply opens an
 * input right here; sending it posts a normal turn linked to the question and leaves it open,
 * since a reply may be a follow-up or a partial answer. Mark answered, Skip, or the desk's
 * question_answer clears it; only then do the buttons go away.
 */
function QuestionActions({
  q, thread, topicId = null, onCleared, onReplied,
}: {
  q: DeskQuestion
  thread: DeskThread
  topicId?: number | null
  /** Called once the question is no longer open (answered or skipped). */
  onCleared: () => void
  /** Called after a reply that left it open. */
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
    <div className="question-actions" role="group" aria-label={`Question ${q.id} is waiting on you`}>
      <span className="question-flag tone-warn">Waiting on you</span>
      {!replying && (
        <>
          <button type="button" className="button" onClick={() => setReplying(true)} disabled={busy}>
            Reply here
          </button>
          <button type="button" className="button" onClick={() => void close('skip')} disabled={busy}>
            Skip
          </button>
          <button type="button" className="button" onClick={() => void close('answer')} disabled={busy}>
            Mark answered
          </button>
        </>
      )}
      {replying && (
        <form
          className="question-reply"
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
          <button type="submit" className="button button-primary" disabled={busy || !text.trim()}>
            {busy ? 'Sending…' : 'Send reply'}
          </button>
          <button type="button" className="button" onClick={() => setReplying(false)} disabled={busy}>
            Cancel
          </button>
        </form>
      )}
      {problem && (
        <p className="tone-bad" role="alert">
          {problem}
        </p>
      )}
    </div>
  )
}

export { QuestionActions }
