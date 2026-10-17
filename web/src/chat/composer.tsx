import { useEffect, useRef, useState } from 'react'
import { DRAFT_EVENT, clearDraft, draftKey, readDraft, writeDraft } from '../model/chatDraft'
import { DeskThread, type DeskSnapshot } from '../state/desk'
import { Button, ContextComposer, OFFLINE_SEND, useOffline } from '../ui'

function Composer({
  thread, workspaceId, d, disabled, topicId, onSend,
}: {
  thread: DeskThread
  workspaceId: number
  d: DeskSnapshot
  disabled: boolean
  /** The topic being viewed: the turn is filed under it. */
  topicId: number | null
  /** Called as a message goes out, so the thread follows it to the bottom. */
  onSend: () => void
}) {
  const key = draftKey(workspaceId)
  // The words are kept as they are typed, so leaving the page and coming back loses nothing.
  const [text, setText] = useState(() => readDraft(window.localStorage, key))
  const edit = (v: string) => {
    setText(v)
    writeDraft(window.localStorage, key, v)
  }
  const box = useRef<HTMLTextAreaElement>(null)
  const sending = d.sending
  const { offline } = useOffline()
  // Words put in the draft from elsewhere (the rail's Start) show at once.
  useEffect(() => {
    const onDraft = (e: Event) => {
      if ((e as CustomEvent<string>).detail !== key) return
      setText(readDraft(window.localStorage, key))
      box.current?.focus()
    }
    window.addEventListener(DRAFT_EVENT, onDraft)
    return () => window.removeEventListener(DRAFT_EVENT, onDraft)
  }, [key])

  const send = async () => {
    if (sending || disabled || offline || !text.trim()) return
    // The text stays until the daemon has taken it, so a refusal loses nothing.
    onSend()
    if (await thread.send(text, topicId)) {
      setText('')
      clearDraft(window.localStorage, key)
    }
    box.current?.focus()
  }

  // "Ask the desk first" on a decision page sends the person here.
  useEffect(() => {
    const focus = () => box.current?.focus()
    window.addEventListener('shepherd:focus-context-composer', focus)
    return () => window.removeEventListener('shepherd:focus-context-composer', focus)
  }, [])

  return (
    <form
      className="sh-conv-composer"
      onSubmit={(e) => {
        e.preventDefault()
        void send()
      }}
    >
      {d.problem && (
        <p className="sh-conv-problem" role="alert">
          {d.problem.message}
        </p>
      )}
      {d.notice && !d.problem && (
        <p className="sh-conv-notice" role="status">
          {d.notice}
        </p>
      )}
      <ContextComposer variant="composer">
        <label htmlFor="chat-input" className="visually-hidden">
          Message to the front desk
        </label>
        <textarea
          id="chat-input"
          ref={box}
          rows={2}
          value={text}
          readOnly={sending}
          disabled={disabled}
          placeholder="Message the front desk. Enter sends, Shift+Enter adds a line."
          onChange={(e) => edit(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send()
            }
          }}
        />
        {d.busy && (
          <Button onClick={() => void thread.interrupt()}>Interrupt</Button>
        )}
        <Button type="submit" variant="primary" disabled={sending || disabled || offline || !text.trim()} title={offline ? OFFLINE_SEND : undefined}>
          {sending ? 'Sending…' : 'Send'}
        </Button>
      </ContextComposer>
    </form>
  )
}

export { Composer }
