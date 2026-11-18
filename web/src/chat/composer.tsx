import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent } from 'react'
import { DRAFT_EVENT, clearDraft, draftKey, readDraft, writeDraft } from '../model/chatDraft'
import { DeskThread, type DeskSnapshot } from '../state/desk'
import { Button, Chip, ContextComposer, OFFLINE_SEND, useOffline } from '../ui'
import { refusal, sizeText } from './attachments'

/** A file waiting to go with the message: uploading, kept (with its id) or refused (with the words). */
interface Pending {
  key: number
  name: string
  size: number
  state: 'uploading' | 'kept' | 'failed'
  id?: number
  error?: string
  preview?: string
}

let nextKey = 1

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

  const [files, setFiles] = useState<Pending[]>([])
  const [drop, setDrop] = useState(false)
  const picker = useRef<HTMLInputElement>(null)
  const kept = files.filter((f) => f.state !== 'failed')
  const uploading = files.some((f) => f.state === 'uploading')
  const ready = files.filter((f) => f.state === 'kept' && f.id !== undefined).map((f) => f.id as number)
  const canSend = !sending && !disabled && !offline && !uploading && (text.trim() !== '' || ready.length > 0)
  const patch = (k: number, p: Partial<Pending>) => setFiles((fs) => fs.map((f) => (f.key === k ? { ...f, ...p } : f)))

  // Each file goes up as it is added, so a refusal shows at once and Send waits for the rest.
  const add = (list: File[]) => {
    let count = kept.length
    let bytes = kept.reduce((n, f) => n + f.size, 0)
    for (const file of list) {
      const k = nextKey++
      const why = refusal(file, { count, bytes })
      if (why) {
        setFiles((fs) => [...fs, { key: k, name: file.name, size: file.size, state: 'failed', error: why }])
        continue
      }
      count++
      bytes += file.size
      const preview = file.type.startsWith('image/') && typeof URL.createObjectURL === 'function' ? URL.createObjectURL(file) : undefined
      setFiles((fs) => [...fs, { key: k, name: file.name, size: file.size, state: 'uploading', preview }])
      thread.attachFile(file).then(
        (a) => patch(k, { state: 'kept', id: a.id }),
        (e) => patch(k, { state: 'failed', error: e instanceof Error ? e.message : String(e) }),
      )
    }
  }
  const drop1 = (k: number) => setFiles((fs) => fs.filter((f) => f.key !== k))
  useEffect(() => () => files.forEach((f) => f.preview && URL.revokeObjectURL(f.preview)), [])  // eslint-disable-line react-hooks/exhaustive-deps
  const onPaste = (e: ClipboardEvent) => {
    const list = Array.from(e.clipboardData?.files ?? [])
    if (list.length === 0) return
    e.preventDefault()
    add(list)
  }
  const onDrop = (e: DragEvent) => {
    setDrop(false)
    const list = Array.from(e.dataTransfer?.files ?? [])
    if (list.length === 0) return
    e.preventDefault()
    add(list)
  }
  const dragging = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes('Files')

  const send = async () => {
    if (!canSend) return
    // The text stays until the daemon has taken it, so a refusal loses nothing.
    onSend()
    if (await thread.send(text, topicId, undefined, undefined, ready)) {
      setText('')
      clearDraft(window.localStorage, key)
      files.forEach((f) => f.preview && URL.revokeObjectURL(f.preview))
      setFiles([])
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
      className={drop ? 'sh-conv-composer is-drop' : 'sh-conv-composer'}
      onDragEnter={(e) => dragging(e) && setDrop(true)}
      onDragOver={(e) => {
        if (!dragging(e)) return
        e.preventDefault()
        setDrop(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDrop(false)
      }}
      onDrop={onDrop}
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
      {files.length > 0 && (
        <ul className="sh-attach-bar" aria-label="Files to send">
          {files.map((f) => (
            <li key={f.key} className={f.state === 'failed' ? 'sh-attach-item is-bad' : 'sh-attach-item'}>
              {f.preview && <img className="sh-attach-thumb" src={f.preview} alt="" />}
              <Chip kind="file" onRemove={() => drop1(f.key)}>
                <span aria-hidden="true">{f.state === 'failed' ? '✗ ' : f.state === 'uploading' ? '● ' : '▤ '}</span>
                {f.name} <span className="sh-attach-size">{sizeText(f.size)}</span>
                {f.state === 'uploading' && ' uploading'}
              </Chip>
              {f.error && <span className="sh-attach-hint" role="alert">{f.error}</span>}
            </li>
          ))}
        </ul>
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
          onPaste={onPaste}
          onChange={(e) => edit(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send()
            }
          }}
        />
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          aria-label="Choose files to attach"
          onChange={(e) => {
            add(Array.from(e.target.files ?? []))
            e.target.value = ''
          }}
        />
        <Button onClick={() => picker.current?.click()} disabled={disabled || sending} title="Attach files, or paste or drop them here">
          Attach
        </Button>
        {d.busy && (
          <Button onClick={() => void thread.interrupt()}>Interrupt</Button>
        )}
        <Button type="submit" variant="primary" disabled={!canSend} title={offline ? OFFLINE_SEND : undefined}>
          {sending ? 'Sending…' : 'Send'}
        </Button>
      </ContextComposer>
    </form>
  )
}

export { Composer }
