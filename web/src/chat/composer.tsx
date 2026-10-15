import { useEffect, useRef, useState } from 'react'
import { DRAFT_EVENT, clearDraft, draftKey, readDraft, writeDraft } from '../model/chatDraft'
import { DeskThread, type DeskSnapshot } from '../state/desk'

function Composer({
  thread, workspaceId, d, disabled, following, topicId, onSend,
}: {
  thread: DeskThread
  workspaceId: number
  d: DeskSnapshot
  disabled: boolean
  following: boolean
  /** The topic being viewed: the turn is filed under it. */
  topicId: number | null
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
    if (sending || disabled || !text.trim()) return
    // The text stays until the daemon has taken it, so a refusal loses nothing.
    onSend()
    if (await thread.send(text, topicId)) {
      setText('')
      clearDraft(window.localStorage, key)
    }
    box.current?.focus()
  }

  return (
    <form
      className="composer"
      onSubmit={(e) => {
        e.preventDefault()
        void send()
      }}
    >
      {!following && (
        <button type="button" className="button chat-jump" onClick={onSend}>
          Jump to latest
        </button>
      )}
      {d.problem && (
        <p className="chat-problem tone-bad" role="alert">
          {d.problem.message}
        </p>
      )}
      {d.notice && !d.problem && (
        <p className="chat-notice faint" role="status">
          {d.notice}
        </p>
      )}
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
      <div className="composer-bar">
        {d.busy && (
          <button type="button" className="button" onClick={() => void thread.interrupt()}>
            Interrupt
          </button>
        )}
        <button type="submit" className="button button-primary" disabled={sending || disabled || !text.trim()}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>
    </form>
  )
}

export { Composer }
