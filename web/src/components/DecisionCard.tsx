import { useEffect, useId, useRef, useState } from 'react'
import type { ArtifactLink, Decision, DecisionView } from '../api/types'
import { Markdown, inline } from '../conversation/Markdown'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { answerKey, clearDraft, readDraft, writeDraft } from '../model/chatDraft'
import { href, withOpen } from '../router'
import { useLive } from '../state/context'
import '../styles/conversation.css'
import '../styles/decisions.css'
import { Button, Chip, useToast } from '../ui'
import { Glyph } from './Glyph'

function artifactsOf(d: DecisionView): ArtifactLink[] {
  // The daemon emits artifact ids starting at 1; 0 is omitted because it would not
  // be a valid artifact id in the store and would break the artifact view.
  return (d.artifacts ?? []).filter((a) => a.id > 0)
}

/** A question longer than this (characters or lines) starts clamped, with a "Show all". */
export const LONG_CHARS = 700
export const LONG_LINES = 14
/** A first line longer than this is not a summary; the whole text is the body. */
const SUMMARY_MAX = 200

/**
 * Splits a question into the one-line summary it should open with and the rest. Agents
 * are asked to lead with a summary; when the first line is not one, there is no headline
 * and the whole text is the body.
 */
export function splitQuestion(q: string): { head: string; body: string } {
  const text = q.replace(/\r\n?/g, '\n').trim()
  const nl = text.indexOf('\n')
  const first = (nl < 0 ? text : text.slice(0, nl)).trim()
  const plain = first.replace(/^#{1,6}\s+/, '')
  if (plain.length === 0 || plain.length > SUMMARY_MAX || /^(```|\s*(?:[-*]|\d+[.)])\s)/.test(first)) {
    return { head: '', body: text }
  }
  return { head: plain, body: nl < 0 ? '' : text.slice(nl + 1).trim() }
}

export function isLong(text: string) {
  return text.length > LONG_CHARS || text.split('\n').length > LONG_LINES
}

type Sent = { state: 'idle' } | { state: 'sending' } | { state: 'done'; d: Decision } | { state: 'error'; msg: string }

/**
 * One decision, the same on the Decisions page and in the chat thread: who asked, the
 * question as markdown with a headline, the options with the recommended one marked, and
 * the person's answer. Only the person's own click sends it.
 */
export function DecisionCard({
  d, laneId, focused = false, variant = 'page', onSent,
}: {
  d: DecisionView
  laneId?: number
  focused?: boolean
  /** `inline` is the compact card in the thread; `pane` the full view in the inspector. */
  variant?: 'page' | 'inline' | 'pane'
  /** Called once the daemon has taken the answer. */
  onSent?: (d: Decision) => void
}) {
  const live = useLive()
  const toast = useToast()
  const now = useNow()
  const options = d.options ?? []
  const inlineCard = variant === 'inline'
  const pane = variant === 'pane'
  const draft = answerKey(d.id)
  const [choice, setChoice] = useState('')
  const [own, setOwnText] = useState(() => readDraft(window.localStorage, draft))
  const [sent, setSent] = useState<Sent>({ state: 'idle' })
  const [expanded, setExpanded] = useState(false)
  const ref = useRef<HTMLElement>(null)
  const uid = useId()
  const id = `decision-${d.id}-${uid.replace(/:/g, '')}`
  const mac = navigator.userAgent.includes('Mac')
  const artifacts = inlineCard ? artifactsOf(d) : []
  const canSendHere = !inlineCard // inline sends via its own visible row, not the global listener

  const setOwn = (v: string) => {
    setOwnText(v)
    writeDraft(window.localStorage, draft, v)
  }

  useEffect(() => {
    if (focused) ref.current?.scrollIntoView?.({ block: 'nearest' })
  }, [focused])

  // Number keys pick an option of the focused decision; they never send it.
  useKeys(
    Object.fromEntries(
      options.slice(0, 9).map((o, i) => [String(i + 1), () => focused && (setChoice(o), setOwn(''))]),
    ),
    [focused, options.join('\n')],
  )

  const words = own.trim() || choice
  const send = async () => {
    if (!words || sent.state !== 'idle') return
    setSent({ state: 'sending' })
    try {
      const done = await live.answer(d.id, words)
      clearDraft(window.localStorage, draft)
      setSent({ state: 'done', d: done })
      toast(done.answer_run ? `Answered decision ${d.id}. The agent carries on as run ${done.answer_run}.` : `Answered decision ${d.id}. The agent gets it when its run ends.`, 'ok')
      onSent?.(done)
    } catch (e) {
      setSent({ state: 'error', msg: e instanceof Error ? e.message : String(e) })
    }
  }

  // Cmd+Enter or Ctrl+Enter sends the card that shows a textarea or option buttons with
  // its own visible send control. The inline card uses its own per-choice send row, so the
  // window listener is not attached there.
  const sendRef = useRef(send)
  useEffect(() => {
    sendRef.current = send
  })
  useEffect(() => {
    if (!focused || !canSendHere) return
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && !e.defaultPrevented) {
        // A box elsewhere on the page (the composer) keeps its own Cmd+Enter.
        const t = e.target
        if (t instanceof HTMLElement && t !== document.body && !ref.current?.contains(t) && t.closest('textarea, input, [contenteditable]')) return
        e.preventDefault()
        void sendRef.current()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [focused, canSendHere])

  const { head, body: fullBody } = splitQuestion(d.question)
  // The compact card is the headline and the options; the body is for the inspector.
  const body = inlineCard ? (head ? '' : clip(fullBody, 240)) : fullBody
  // The inspector is where the whole body is read, so it is never clamped there.
  const long = !pane && isLong(body)
  const recIsOption = !!d.recommendation && options.some((o) => o.trim() === d.recommendation?.trim())
  const Tag = inlineCard || pane ? 'section' : 'article'
  const labelled = head ? { 'aria-labelledby': `${id}-q` } : { 'aria-label': `Decision ${d.id}` }

  const content = (
    <div className="decision-body">
      {inlineCard ? (
        <p className="decision-meta">
          <span className="meta-kind">Decision {d.id}</span>
          <span aria-hidden="true" className="meta-sep">·</span>
          {laneId !== undefined ? (
            <a href={href.lane(laneId)} className="meta-lane">{d.lane}</a>
          ) : (
            <span className="meta-lane">{d.lane}</span>
          )}
          <span aria-hidden="true" className="meta-sep">·</span>
          <span className="meta-agent">{d.agent}, <a href={href.log(d.run_id)}>run {d.run_id}</a></span>
          <span aria-hidden="true" className="meta-sep">·</span>
          <span className="meta-age">{since(d.created, now)}</span>
          {artifacts.length > 0 && (
            <>
              <span aria-hidden="true" className="meta-sep">·</span>
              {artifacts.map((a) => (
                <a
                  key={a.id}
                  className="meta-artifact"
                  href={withOpen(window.location.hash, { kind: 'artifact', id: a.id })}
                >
                  artifact {a.id}
                </a>
              ))}
            </>
          )}
          <a className="meta-open" href={withOpen(window.location.hash, { kind: 'decision', id: d.id })}>open</a>
        </p>
      ) : (
        <p className="decision-meta">
          <span>decision {d.id}</span>
          {laneId !== undefined ? <a href={href.lane(laneId)}>{d.lane}</a> : <span>{d.lane}</span>}
          <span>{d.agent}</span>
          <a href={href.log(d.run_id)}>run {d.run_id}</a>
          <span className="faint">{d.repo}</span>
          <span className="faint">waiting {since(d.created, now)}</span>
        </p>
      )}
      {head &&
        (inlineCard || pane ? (
          <p id={`${id}-q`} className="decision-q">{inline(head)}</p>
        ) : (
          <h2 id={`${id}-q`} className="decision-q">{inline(head)}</h2>
        ))}
      {body && inlineCard && <p className="decision-q">{inline(body)}</p>}
      {body && !inlineCard && (
        <div className="decision-text">
          <div id={`${id}-text`} className={`decision-md${long && !expanded ? ' is-clamped' : ''}`}>
            <Markdown text={body} />
          </div>
          {long && (
            <button
              type="button"
              className="link-button decision-more"
              aria-expanded={expanded}
              aria-controls={`${id}-text`}
              onClick={() => setExpanded((e) => !e)}
            >
              {expanded ? 'Show less' : `Show all (${body.split('\n').length} lines)`}
            </button>
          )}
        </div>
      )}
      {d.why && pane && (
        <p className="decision-why-line">
          <strong>Why this is yours:</strong> {d.why}
        </p>
      )}
      {d.why && !inlineCard && !pane && (
        <aside className="decision-aside decision-why" aria-label="Why this is your call">
          <span className="decision-aside-h">Why this is yours</span>
          <Markdown text={d.why} />
        </aside>
      )}
      {d.recommendation && !recIsOption && !inlineCard && (
        <aside className="decision-aside decision-rec" aria-label="Recommendation">
          <span className="decision-aside-h">Recommended</span>
          <Markdown text={d.recommendation} />
        </aside>
      )}

      {sent.state === 'done' ? (
        <p className="decision-done" role="status">
          <Glyph glyph="↳" tone="ok" label="answered" /> Answered “{sent.d.answer ?? words}”.
          {sent.d.answer_run ? (
            <>
              {' '}The agent carries on as <a href={href.log(sent.d.answer_run)}>run {sent.d.answer_run}</a>.
            </>
          ) : (
            ' The agent gets it when its run ends.'
          )}
        </p>
      ) : (
        <fieldset className="decision-answer" disabled={sent.state === 'sending'}>
          <legend className="visually-hidden">Your answer to decision {d.id}</legend>
          {options.length > 0 && inlineCard && (
            <div className="decision-options" role="group" aria-label={`Options for decision ${d.id}`}>
              {options.map((o, i) => (
                <Chip
                  key={o}
                  selected={choice === o}
                  onClick={() => {
                    setChoice(o)
                    setOwn('')
                  }}
                >
                  <span aria-hidden="true" className="option-num">{i + 1}.</span>
                  <span className="option-text">{inline(o)}</span>
                </Chip>
              ))}
              {choice && (
                <div className="inline-send">
                  <Button size="small" variant="attention" disabled={sent.state !== 'idle'} onClick={() => void send()}>
                    {sent.state === 'sending' ? 'Sending…' : `Send “${clip(choice, 24)}”`}
                  </Button>
                  <span className="send-hint faint">or open to edit</span>
                </div>
              )}
            </div>
          )}
          {options.length === 0 && inlineCard && (
            <p className="send-hint faint">Open this decision to write your answer.</p>
          )}
          {inlineCard && sent.state === 'error' && (
            <span className="tone-bad" role="alert">
              Not sent: {sent.msg}
            </span>
          )}
          {options.length > 0 && !inlineCard && (
            <ol className="options">
              {options.map((o, i) => {
                const rec = !!d.recommendation && o.trim() === d.recommendation.trim()
                return (
                  <li key={o}>
                    <button
                      type="button"
                      className={`option${rec ? ' is-recommended' : ''}`}
                      aria-pressed={choice === o && !own.trim()}
                      onClick={() => {
                        setChoice(o)
                        setOwn('')
                      }}
                    >
                      {i < 9 && (!inlineCard || focused) && <kbd aria-hidden="true">{i + 1}</kbd>}
                      <span className="option-text">{inline(o)}</span>
                      {rec && <span className="option-rec">recommended</span>}
                    </button>
                  </li>
                )
              })}
            </ol>
          )}
          {!inlineCard && (
            <label className="own">
              <span className="visually-hidden">Or write your own answer to decision {d.id}</span>
              <textarea
                rows={own ? 3 : 1}
                placeholder={options.length ? 'Or write your own answer' : 'Write your answer'}
                value={own}
                onChange={(e) => setOwn(e.target.value)}
              />
            </label>
          )}
          {!inlineCard && (
            <div className="send">
              {pane ? (
                <>
                  <Button variant="attention" disabled={!words} onClick={() => void send()}>
                    {sent.state === 'sending' ? 'Sending…' : 'Send answer'}
                  </Button>
                  <Button variant="secondary" onClick={() => window.dispatchEvent(new CustomEvent('shepherd:focus-context-composer'))}>
                    Ask the desk first
                  </Button>
                  <span className="send-hint faint">
                    <kbd>{mac ? '⌘↵' : 'Ctrl+Enter'}</kbd> send, then next
                  </span>
                </>
              ) : (
                <Button variant="primary" disabled={!words} onClick={() => void send()}>
                  {sent.state === 'sending' ? 'Sending…' : words ? `Send “${clip(words, 48)}”` : 'Send answer'}
                </Button>
              )}
              {sent.state === 'error' && (
                <span className="tone-bad" role="alert">
                  Not sent: {sent.msg}
                </span>
              )}
            </div>
          )}
        </fieldset>
      )}
    </div>
  )

  return (
    <Tag
      ref={ref as never}
      className={`decision${inlineCard ? ' decision-inline' : ''}${pane ? ' decision-pane' : ''}${focused ? ' is-focused' : ''}`}
      {...labelled}
    >
      {!inlineCard && !pane && <Glyph glyph="?" tone="warn" label="decision waiting" />}
      {content}
    </Tag>
  )
}

function clip(s: string, n: number) {
  const one = s.split(/\s+/).join(' ')
  return one.length > n ? one.slice(0, n - 1) + '…' : one
}
