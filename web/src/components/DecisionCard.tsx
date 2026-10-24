import { useEffect, useRef, useState } from 'react'
import type { ArtifactLink, Decision, DecisionView } from '../api/types'
import { useDecisionPlan } from '../artifacts/InlineArtifact'
import { Markdown, inline } from '../conversation/Markdown'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { answerKey, clearDraft, readDraft, writeDraft } from '../model/chatDraft'
import { go, href, withOpen } from '../router'
import { useLive } from '../state/context'
import { IdChip, StatusGlyph, useToast } from '../ui'
import { DecisionCard as CardView } from '../ui/DecisionCard'
import { DecisionView as PageView } from '../ui/DecisionView'

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
 * One decision, the same in the thread and in the main column: who asked, the question as
 * markdown with a headline, the options with the recommended one marked, and the person's
 * answer. `inline` is the compact card of the thread (`DecisionCard` part); `page` and `pane`
 * are the whole decision (`DecisionView` part). Only the person's own click sends it.
 */
export function DecisionCard({
  d, laneId, focused = false, variant = 'page', onSent, unseen = false, selected = false, mobile = false, hasNext = false,
}: {
  d: DecisionView
  laneId?: number
  focused?: boolean
  /** `inline` is the compact card in the thread; `page` and `pane` the whole decision. */
  variant?: 'page' | 'inline' | 'pane'
  /** Called once the daemon has taken the answer. */
  onSent?: (d: Decision) => void
  /** The decision waits on the person and has not been opened. */
  unseen?: boolean
  /** The decision is the one open in the main column. */
  selected?: boolean
  /** The 390px page: options and buttons at least 44px tall. */
  mobile?: boolean
  /** Another decision waits after this one, so `]` goes to it. */
  hasNext?: boolean
}) {
  const live = useLive()
  const toast = useToast()
  const now = useNow()
  const options = d.options ?? []
  const inlineCard = variant === 'inline'
  const draft = answerKey(d.id)
  const [choice, setChoice] = useState('')
  const [own, setOwnText] = useState(() => readDraft(window.localStorage, draft))
  const [sent, setSent] = useState<Sent>({ state: 'idle' })
  const [expanded, setExpanded] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const artifacts = artifactsOf(d)
  const plan = useDecisionPlan(inlineCard ? undefined : artifacts[0])

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

  // The desktop inline card shows only the chip choice, so it never sends a draft kept for the page;
  // the 390px card shows the own-answer field, and what is typed there wins over a picked chip.
  const words = inlineCard && !mobile ? choice : own.trim() || choice
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

  // Cmd+Enter or Ctrl+Enter sends the page that shows a textarea and a visible send control.
  // The inline card sends only by its own button: several cards share a thread.
  const sendRef = useRef(send)
  useEffect(() => {
    sendRef.current = send
  })
  useEffect(() => {
    if (!focused || inlineCard) return
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
  }, [focused, inlineCard])

  const { head, body: fullBody } = splitQuestion(d.question)
  const recommended = d.recommendation?.trim() && options.some((o) => o.trim() === d.recommendation?.trim()) ? d.recommendation : undefined
  const label = `Decision ${d.id}`
  const error = sent.state === 'error' ? sent.msg : undefined

  const answered =
    sent.state === 'done' ? (
      <p className={inlineCard ? 'sh-card-done' : 'sh-decision-done'} role="status">
        <StatusGlyph glyph="↳" tone="ok" label="answered" /> <span>Answered “{sent.d.answer ?? words}”.{' '}
        {sent.d.answer_run ? (
          <>The agent carries on as <a href={href.log(sent.d.answer_run)}>run {sent.d.answer_run}</a>.</>
        ) : (
          'The agent gets it when its run ends.'
        )}</span>
      </p>
    ) : undefined

  if (inlineCard) {
    // The compact card is the headline and the options; the body is for the page.
    const question = head || clip(fullBody, 240)
    return (
      <div ref={ref}>
        <CardView
          label={label}
          unseen={unseen}
          selected={selected}
          mobile={mobile}
          onOpen={() => go(withOpen(window.location.hash, { kind: 'decision', id: d.id }))}
          meta={
            <>
              <IdChip kind="decision" id={d.id} />
              {laneId !== undefined ? <a href={href.lane(laneId)}>{d.lane}</a> : <span>{d.lane}</span>}
              <span>
                {d.agent}, <a href={href.log(d.run_id)}>run {d.run_id}</a>
              </span>
              <span>{since(d.created, now)}</span>
            </>
          }
          question={inline(question)}
          options={options}
          recommended={recommended}
          picked={choice}
          onPick={setChoice}
          onSend={options.length > 0 || mobile ? () => void send() : undefined}
          sending={sent.state === 'sending'}
          sendLabel={`“${clip(words, 24)}”`}
          own={mobile ? own : undefined}
          onOwn={mobile ? (t) => { setOwn(t); setChoice('') } : undefined}
          done={answered}
          error={error}
          more={
            <>
              {artifacts.map((a) => (
                <a key={a.id} className="sh-file" href={withOpen(window.location.hash, { kind: 'artifact', id: a.id })} title={`artifact ${a.id}`}>
                  {a.path}
                </a>
              ))}
              {options.length === 0 && <span className="sh-card-hint">Open this decision to write your answer.</span>}
              <a className="sh-link" href={withOpen(window.location.hash, { kind: 'decision', id: d.id })}>
                Open the full decision
              </a>
            </>
          }
        />
      </div>
    )
  }

  // The whole body is read on the page, clamped when very long.
  const long = variant !== 'pane' && isLong(fullBody)
  return (
    <div ref={ref}>
      <PageView
        layout={mobile ? 'mobile' : 'page'}
        label={label}
        kind={
          <>
            <IdChip kind="decision" id={d.id} />, {d.agent} on {laneId !== undefined ? <a href={href.lane(laneId)}>{d.lane}</a> : d.lane} (<a href={href.log(d.run_id)}>run {d.run_id}</a>
            , {d.repo}), waiting {since(d.created, now)}
          </>
        }
        question={head ? inline(head) : label}
        why={d.why ? <Markdown text={d.why} /> : undefined}
        context={
          fullBody ? (
            <>
              <div id={`decision-${d.id}-text`} className={`sh-decision-md${long && !expanded ? ' is-clamped' : ''}`}>
                <Markdown text={fullBody} />
              </div>
              {long && (
                <button type="button" className="sh-link sh-decision-more" aria-expanded={expanded} aria-controls={`decision-${d.id}-text`} onClick={() => setExpanded((e) => !e)}>
                  {expanded ? 'Show less' : `Show all (${fullBody.split('\n').length} lines)`}
                </button>
              )}
            </>
          ) : undefined
        }
        recommendation={d.recommendation && !recommended ? <Markdown text={d.recommendation} /> : undefined}
        options={options}
        recommended={recommended}
        picked={own.trim() ? undefined : choice}
        onPick={(o) => {
          setChoice(o)
          setOwn('')
        }}
        plan={plan}
        own={own}
        onOwn={setOwn}
        onSend={() => void send()}
        canSend={!!words}
        sending={sent.state === 'sending'}
        onAsk={() => window.dispatchEvent(new CustomEvent('shepherd:focus-context-composer'))}
        hasNext={hasNext}
        done={answered}
        error={error}
      />
    </div>
  )
}

function clip(s: string, n: number) {
  const one = s.split(/\s+/).join(' ')
  return one.length > n ? one.slice(0, n - 1) + '…' : one
}
