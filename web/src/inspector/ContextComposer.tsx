import { useEffect, useMemo, useRef, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import type { TopicView } from '../model/topic'
import { DeskThread } from '../state/desk'
import { aboutLine, topicFor, withReference, type About } from '../state/deskAbout'
import '../styles/composer.css'

const defaultMake = (w: number) => new DeskThread(w)

/**
 * The inspector's foot: a message to the front desk about the open entity. It speaks to the
 * desk, never to the lane's agent. The turn carries a reference line and is filed under the
 * entity's topic when one links it.
 */
export function ContextComposer(props: Props) {
  // A different entity starts clean: words and the last outcome are about the one before.
  return <Composer key={`${props.about.kind}:${props.about.ids.join(',')}`} {...props} />
}

interface Props {
  about: About
  /** 0 leaves the workspace out, which the daemon takes as its only one. */
  workspaceId?: number
  make?: (workspaceId: number) => DeskThread
}

function Composer({ about, workspaceId = 0, make = defaultMake }: Props) {
  const thread = useMemo(() => make(workspaceId), [make, workspaceId])
  const [text, setText] = useState('')
  const [topics, setTopics] = useState<TopicView[]>([])
  const [sending, setSending] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [sent, setSent] = useState(false)
  const box = useRef<HTMLTextAreaElement>(null)
  const key = `${about.kind}:${about.ids.join(',')}`

  useEffect(() => {
    let live = true
    topicsApi.list().then(
      (l) => live && setTopics(l.topics),
      () => live && setTopics([]),
    )
    return () => {
      live = false
    }
  }, [key])
  // The `c` key and other callers focus the box by this event.
  useEffect(() => {
    const focus = () => box.current?.focus()
    window.addEventListener('shepherd:focus-context-composer', focus)
    return () => window.removeEventListener('shepherd:focus-context-composer', focus)
  }, [])

  const send = async () => {
    if (sending || !text.trim()) return
    setSending(true)
    setProblem(null)
    setSent(false)
    const ok = await thread.send(withReference(about, text), topicFor(about, topics))
    setSending(false)
    if (ok) {
      setText('')
      setSent(true)
    } else {
      setProblem(thread.getSnapshot().problem?.message ?? 'The front desk did not take that. Your words are kept; try again.')
    }
    box.current?.focus()
  }

  return (
    <form
      className="context-composer"
      onSubmit={(e) => {
        e.preventDefault()
        void send()
      }}
    >
      <label htmlFor="context-composer-input" className="context-composer-about">
        Message the front desk about {aboutLine(about)}
      </label>
      <textarea
        id="context-composer-input"
        ref={box}
        rows={2}
        value={text}
        readOnly={sending}
        placeholder="Enter sends, Shift+Enter adds a line."
        onChange={(e) => {
          setText(e.target.value)
          setSent(false)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            void send()
          }
        }}
      />
      {problem && (
        <p className="tone-bad" role="alert">
          {problem}
        </p>
      )}
      {sent && !problem && (
        <p className="faint" role="status">
          Sent to the front desk. Its reply is in the conversation.
        </p>
      )}
      <div className="context-composer-bar">
        <button type="submit" className="button button-primary" disabled={sending || !text.trim()}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>
    </form>
  )
}
