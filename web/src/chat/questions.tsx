import { useEffect, useRef, useState } from 'react'
import type { DeskQuestion } from '../api/types'
import { deskQuestions } from '../api/client'
import { DeskThread } from '../state/desk'

/**
 * Reply here and Skip under a desk message that asked the person something. Reply opens an
 * input right here; sending it posts a normal turn linked to the question, which clears it.
 * Skip clears it without an answer. Once cleared the question is no longer open, so the
 * buttons go away.
 */
function QuestionActions({
  q, thread, topicId = null, onCleared,
}: {
  q: DeskQuestion
  thread: DeskThread
  topicId?: number | null
  onCleared: () => void
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
      onCleared()
    } else {
      setProblem('Not sent. The desk may be busy; try again.')
    }
  }
  const skip = async () => {
    setBusy(true)
    setProblem(null)
    try {
      await deskQuestions.skip(q.id)
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
          <button type="button" className="button" onClick={() => void skip()} disabled={busy}>
            Skip
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
